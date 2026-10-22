import { useEffect, useState } from 'react';
import * as App from '../../wailsjs/go/main/App';

const btn =
  'text-sm border rounded px-3 py-1 hover:bg-slate-50 dark:hover:bg-slate-700 dark:border-slate-600';

/** Botões de controle da janela: minimizar, maximizar/restaurar, tela cheia e fechar. */
export default function JanelaControles() {
  const [maximizada, setMaximizada] = useState(false);

  useEffect(() => {
    App.JanelaMaximizada().then(setMaximizada).catch(() => {});
  }, []);

  const alternarMaximizar = () => {
    App.AlternarMaximizarJanela();
    App.JanelaMaximizada().then(setMaximizada).catch(() => {});
  };

  return (
    <div className="flex gap-2">
      <button className={btn} title="Minimizar" aria-label="Minimizar janela" onClick={() => App.MinimizarJanela()}>
        🗕
      </button>
      <button className={btn} title={maximizada ? 'Restaurar' : 'Maximizar'} aria-label={maximizada ? 'Restaurar janela' : 'Maximizar janela'} onClick={alternarMaximizar}>
        {maximizada ? '🗗' : '🗖'}
      </button>
      <button className={btn} title="Tela cheia" aria-label="Alternar tela cheia" onClick={() => App.AlternarTelaCheia()}>
        ⛶
      </button>
      <button className={btn} title="Fechar" aria-label="Fechar aplicativo" onClick={() => App.FecharApp()}>
        ✕
      </button>
    </div>
  );
}
