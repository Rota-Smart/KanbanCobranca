package orcamento

import "testing"

func TestClassificar(t *testing.T) {
	casos := []struct {
		nome   string
		perfil PerfilCliente
		sele   Selecao
		quer   string
	}{
		{
			"1-3 cameras com contrato de imagens",
			PerfilCliente{ContratoAlarme: true, ContratoImagens: true, TemCameraWifi: true},
			Selecao{TipoSistema: "CAMERAS WIFI", Itens: map[string]int{"Câmera WIFI Interna": 2}},
			TipoService,
		},
		{
			"câmera DVR em cliente só wifi",
			PerfilCliente{ContratoAlarme: true, ContratoImagens: true, TemCameraWifi: true},
			Selecao{TipoSistema: "CAMERAS DVR", Itens: map[string]int{"Câmera Wifi Externa - IM5 (Sem SD)": 1, "DVR 4 Canais": 1}},
			TipoAmpliacao,
		},
		{
			"só câmera bonificada, adiciona câmeras",
			PerfilCliente{ContratoAlarme: true, CameraBonificada: true, TemCameraWifi: true},
			Selecao{TipoSistema: "CAMERAS WIFI", Itens: map[string]int{"Câmera WIFI Externa": 1}},
			TipoAmpliacao,
		},
		{
			"mais de 3 câmeras",
			PerfilCliente{ContratoAlarme: true, ContratoImagens: true, TemCameraDVR: true},
			Selecao{TipoSistema: "CAMERAS DVR", Itens: map[string]int{"Câmera 1120": 6}},
			TipoAmpliacao,
		},
		{
			"sensor/controle com contrato alarme",
			PerfilCliente{ContratoAlarme: true},
			Selecao{TipoSistema: "ALARME", Itens: map[string]int{"Sensor de Presença": 2, "Controle": 1}},
			TipoService,
		},
		{
			"alarme sem contrato de alarme",
			PerfilCliente{},
			Selecao{TipoSistema: "ALARME", Itens: map[string]int{"Central Alarme VT": 1}},
			TipoAmpliacao,
		},
	}
	for _, c := range casos {
		got := Classificar(c.perfil, c.sele)
		if got.Tipo != c.quer {
			t.Errorf("%s: quer %s, veio %s (motivos: %v)", c.nome, c.quer, got.Tipo, got.Motivos)
		}
	}
}
