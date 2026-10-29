# Publica release do RotaSuite a partir da main. Chamado em background pelo hook pre-push.
# Roda inteiro em prioridade baixa (herdada do processo pai) para nao travar maquinas fracas.
# Arquivo deliberadamente sem acentos: PowerShell 5.1 le .ps1 sem BOM como ANSI.
param(
    [Parameter(Mandatory=$true)][string]$Versao,
    [Parameter(Mandatory=$true)][string]$Commit,
    [switch]$DryRun
)
$ErrorActionPreference = 'Stop'
$Log = "$env:USERPROFILE\publicar-rotasuite.log"
$MainDir = 'C:\Rota Dev'
$ReleaseDir = 'C:\Rota Smart'
function Log($m) { ("{0} [v{1}] {2}" -f (Get-Date -Format s), $Versao, $m) | Out-File -Append -Encoding utf8 $Log }

try {
    Log "=== inicio (commit $Commit) ==="
    $tmp = Join-Path $env:TEMP ("rotasuite-build-" + (Get-Date -Format 'yyyyMMdd-HHmmss'))

    # 1. worktree temporaria no commit exato da main empurrada
    git -C $MainDir worktree add --detach $tmp $Commit 2>&1 | Out-Null
    Log "worktree temporaria: $tmp"

    # 2. exe rodando bloqueia o build
    Start-Process taskkill -ArgumentList '/T','/F','/IM','rotasuite.exe' -WindowStyle Hidden -ErrorAction SilentlyContinue
    Log "taskkill rotasuite enviado"

    # 3. build (pesado, mas em prioridade baixa herdada)
    if (-not $DryRun) {
        Push-Location $tmp
        & wails build -clean -trimpath 2>&1 | ForEach-Object { Log "build: $_" }
        Pop-Location
        $exe = Join-Path $tmp 'build\bin\rotasuite.exe'
        if (-not (Test-Path $exe)) { throw "build nao gerou o exe" }
        Log ("exe gerado: " + (Get-Item $exe).Length + " bytes")
    } else {
        $exe = Join-Path $tmp 'build\bin\rotasuite.exe'
        Log "DRY-RUN: build pulado"
    }

    # 4. zip (exe + LEIA-ME) via python; zip nao existe no Git Bash
    $zip = Join-Path $tmp 'rota-suite.zip'
    $leia = Join-Path $tmp 'LEIA-ME.txt'
    if (-not $DryRun) {
        $py = @"
import zipfile, hashlib, sys
exe, leia, destino = sys.argv[1], sys.argv[2], sys.argv[3]
with zipfile.ZipFile(destino, 'w', zipfile.ZIP_DEFLATED) as z:
    z.write(exe, 'rotasuite.exe')
    z.write(leia, 'LEIA-ME.txt')
with zipfile.ZipFile(destino) as z:
    dentro = hashlib.md5(z.read('rotasuite.exe')).hexdigest()
fora = hashlib.md5(open(exe,'rb').read()).hexdigest()
assert dentro == fora, 'zip corrompido'
print('zip ok', dentro)
"@
        $pyFile = Join-Path $tmp 'fazzip.py'
        Set-Content -Encoding ascii $pyFile $py
        $out = & python $pyFile $exe $leia $zip
        Log "zip: $out"
    } else { Log "DRY-RUN: zip pulado" }

    # 5. release worktree precisa estar limpa (outra sessao pode estar nela)
    $st = git -C $ReleaseDir status --porcelain --untracked-files=no
    if ($st) { throw ("worktree release suja (" + $ReleaseDir + ") - publique manualmente") }

    if (-not $DryRun) {
        Copy-Item $exe (Join-Path $ReleaseDir 'rotasuite.exe') -Force
        Copy-Item $exe (Join-Path $ReleaseDir ("rotasuite-" + $Versao + ".exe")) -Force
        Copy-Item $zip (Join-Path $ReleaseDir 'rota-suite.zip') -Force
        Set-Content -Encoding ascii -NoNewline (Join-Path $ReleaseDir 'versao.txt') $Versao
        git -C $ReleaseDir add -f rotasuite.exe ("rotasuite-" + $Versao + ".exe") rota-suite.zip versao.txt
        git -C $ReleaseDir commit -m ("v" + $Versao + ": publicacao automatica (main " + $Commit.Substring(0,7) + ")")
        Log "commit release feito"
    } else { Log "DRY-RUN: copia/commit pulados" }

    # 6. push da release
    if (-not $DryRun) {
        git -C $ReleaseDir push origin release 2>&1 | ForEach-Object { Log "push: $_" }
        Log "=== publicada v$Versao ==="
    }
}
catch {
    Log ("ERRO: " + ($_ | Out-String))
    exit 1
}
finally {
    if (Test-Path $tmp) { git -C $MainDir worktree remove --force $tmp 2>$null; Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue }
    Log "worktree temporaria removida"
}
