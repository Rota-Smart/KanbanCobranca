#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Migração única (Etapa 1): copia o rotasmart.db do PC para o Postgres (Supabase).

Uso:
  python migra_sqlite_supabase.py --sqlite "%APPDATA%\\RotaSmart\\rotasmart.db" ^
      --dsn-arquivo dsn.txt            # dsn.txt contém UMA linha: postgresql://...
  python migra_sqlite_supabase.py --sqlite ... --dsn postgresql://...   # direto
  python migra_sqlite_supabase.py --sqlite ... --dry-run                # só conta e valida

Regras:
- Não usa variável de ambiente: a conexão vem de --dsn ou de um arquivo (--dsn-arquivo).
- Idempotente: pode rodar de novo (OUTRO PC incluído) — sobrescreve linha a linha
  preservando os ids. Vale o conteúdo do banco migrado por último.
- Tabelas do RotaSuite apenas; config também vai (chaves Groq/token incluídas —
  a nuvem tem RLS e o papel rotasuite_api é dedicado).
"""
import argparse
import sqlite3
import sys
from pathlib import Path

import psycopg2

# tabela_sqlite, tabela_pg, chave_primária, colunas_extra_pg (não existem no SQLite)
TABELAS = [
    ("kanban_columns",      "kanban_columns",      "nome",          []),
    ("tickets",             "tickets",             "id",            []),
    ("cobrancas",           "cobrancas",           "id",            []),
    ("movimentacoes",       "movimentacoes",       "id",            []),
    ("tickets_vinculos",    "tickets_vinculos",    "id",            []),
    ("tentativas_contato",  "tentativas_contato",  "id",            []),
    ("comissoes",           "comissoes",           "id",            []),
    ("clientes_360",        "clientes_360",        "cod_service",   ["contratos_json"]),
    ("parametros_negociacao","parametros_negociacao","id",          []),
    ("config",              "config",              "chave",         []),
    ("padroes",             "padroes",             "nome",          []),
    ("chats",               "chats",               "id",            []),
    ("chat_mensagens",      "chat_mensagens",      "id",            []),
    ("chat_auditoria",      "chat_auditoria",      "id",            []),
    ("agente_stats",        "agente_stats",        ["classe", "ferramenta"], []),
]

# colunas booleanas no Postgres que eram 0/1 no SQLite
BOOLS = {"ativa", "recebido"}


def ler_dsn(args) -> str:
    if args.dsn:
        return args.dsn.strip()
    caminho = Path(args.dsn_arquivo)
    if not caminho.is_file():
        sys.exit(f"Arquivo de DSN não encontrado: {caminho}")
    linha = caminho.read_text(encoding="utf-8-sig").strip()
    if not linha.startswith("postgresql://"):
        sys.exit(f"{caminho} deve conter uma linha postgresql://usuario:senha@host/db")
    return linha


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--sqlite", required=True, help="caminho do rotasmart.db")
    ap.add_argument("--dsn", help="postgresql://... direto (senão use --dsn-arquivo)")
    ap.add_argument("--dsn-arquivo", default="dsn.txt", help="arquivo com 1 linha de DSN")
    ap.add_argument("--dry-run", action="store_true", help="só conta e valida, não grava")
    ap.add_argument("--só", help="migra só a tabela informada")
    args = ap.parse_args()

    if not Path(args.sqlite).is_file():
        sys.exit(f"Banco SQLite não encontrado: {args.sqlite}")

    origem = sqlite3.connect(args.sqlite)
    origem.row_factory = sqlite3.Row

    total_geral = 0
    resumo = []
    for tabela_sq, tabela_pg, pk, extras in TABELAS:
        if args.só and tabela_sq != args.só:
            continue
        try:
            linhas = origem.execute(f"SELECT * FROM {tabela_sq}").fetchall()
        except sqlite3.OperationalError as e:
            resumo.append(f"{tabela_sq:24s} PULADA ({e})")
            continue
        if not linhas:
            resumo.append(f"{tabela_sq:24s} 0 linhas")
            continue

        cols_sq = linhas[0].keys()
        cols_pg = list(cols_sq) + [c for c in extras if c not in cols_sq]
        resumo.append(f"{tabela_sq:24s} {len(linhas)} linhas")

        if args.dry_run:
            # valida que dá para converter (booleanos etc.) sem gravar
            for ln in linhas:
                _converter(ln, cols_pg)
            total_geral += len(linhas)
            continue

        dsn = ler_dsn(args)
        with psycopg2.connect(dsn) as alvo:
            with alvo.cursor() as cur:
                _migrar_tabela(cur, tabela_pg, pk, cols_sq, cols_pg, extras, linhas)
        total_geral += len(linhas)

    print("\n".join(resumo))
    acao = "validadas" if args.dry_run else "migradas"
    print(f"\nOK — {total_geral} linhas {acao}. Rode de novo no outro PC com o banco de lá para sobrescrever com os dados reais.")


def _converter(linha, cols_pg):
    """SQLite Row → valores prontos para o Postgres (bools, extras faltantes)."""
    vals = []
    for c in cols_pg:
        v = linha[c] if c in linha.keys() else None
        if c in BOOLS and v is not None:
            v = bool(v)
        vals.append(v)
    return vals


def _migrar_tabela(cur, tabela, pk, cols_sq, cols_pg, extras, linhas):
    # colunas de sync ficam de fora da carga: o Postgres decide (default/trigger)
    sync = {"usuario_id", "atualizado_em", "versao", "apagado"}
    cols = [c for c in cols_pg if c not in sync]
    lista = ", ".join(cols)
    fenos = ", ".join("%s" for _ in cols)
    pks = pk if isinstance(pk, list) else [pk]
    onde = " and ".join(f"{c} = %s" for c in pks)

    for ln in linhas:
        vals = _converter(ln, cols_pg)
        vals = [v for c, v in zip(cols_pg, vals) if c not in sync]
        cur.execute(
            f"insert into public.{tabela} ({lista}) values ({fenos}) "
            f"on conflict ({', '.join(pks)}) do update set "
            + ", ".join(f"{c} = excluded.{c}" for c in cols if c not in pks),
            vals,
        )


if __name__ == "__main__":
    main()
