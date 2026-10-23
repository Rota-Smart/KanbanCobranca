package orcamento

import (
	"fmt"
	"strings"
)

// Classificacao do orçamento: ampliação (vai para o RD — Rota Digital /
// alteração de contrato) ou orçamento service (alteração pequena, feita
// direto pelo Service).
type Classificacao struct {
	Tipo      string   `json:"tipo"`      // "Ampliação (RD)" | "Orçamento Service" | "Indefinido"
	Contratos []string `json:"contratos"` // contratos detectados no cliente
	Motivos   []string `json:"motivos"`
}

// Tipos de classificação.
const (
	TipoAmpliacao  = "Ampliação (RD)"
	TipoService    = "Orçamento Service"
	TipoIndefinido = "Indefinido"
)

// PerfilCliente resume o que o cliente já tem, para classificar o pedido.
type PerfilCliente struct {
	ContratoAlarme   bool // serviço de alarme (locação/monitoramento de alarme)
	ContratoImagens  bool // serviço pago de imagens (monitoramento de imagens/câmera paga)
	CameraBonificada bool // câmera grátis/bonificada no contrato (não conta como contrato de imagens)
	TemCameraWifi    bool // produtos wifi (IM3/IM5/IM7/IM9) no cadastro
	TemCameraDVR     bool // produtos DVR/cabeada no cadastro
}

// Classificar cruza o que foi pedido no orçamento com o perfil do cliente e
// decide entre ampliação (RD) e orçamento service. Regras:
//   - mistura de tecnologia (DVR em cliente wifi ou vice-versa) → RD sempre;
//   - adicionar câmeras sem contrato de imagens (ou só com bonificada) → RD
//     (cria contrato novo);
//   - mais de 3 câmeras → RD (não é alteração pequena);
//   - até 3 câmeras, mesma tecnologia e com contrato de imagens → service;
//   - adicionais de alarme com contrato de alarme → service.
func Classificar(p PerfilCliente, s Selecao) *Classificacao {
	c := &Classificacao{Tipo: TipoIndefinido}
	c.Contratos = contratosDoPerfil(p)

	qtdDVR, qtdWifi, temAlarme, temRast := classificarItens(s)
	qtdCameras := qtdDVR + qtdWifi

	rd := func(motivo string) {
		c.Tipo = TipoAmpliacao
		c.Motivos = append(c.Motivos, motivo)
	}

	switch {
	case qtdDVR > 0 && p.TemCameraWifi && !p.TemCameraDVR:
		rd("cliente tem câmeras wifi e o pedido inclui câmera/gravador DVR — mistura de tecnologia, vai obrigatoriamente para o RD")
	case qtdWifi > 0 && p.TemCameraDVR && !p.TemCameraWifi:
		rd("cliente tem câmeras DVR e o pedido inclui câmera wifi — mistura de tecnologia, vai obrigatoriamente para o RD")
	}

	if c.Tipo == TipoIndefinido && qtdCameras > 0 && !p.ContratoImagens {
		if p.CameraBonificada {
			rd("cliente só tem câmera bonificada (não conta como contrato de imagens) — adicionar câmeras cria contrato novo, vai para o RD")
		} else {
			rd("cliente não tem contrato de imagens — ampliação de contrato, vai para o RD")
		}
	}

	if c.Tipo == TipoIndefinido && qtdCameras > 3 {
		rd(fmt.Sprintf("%d câmeras não é alteração pequena (limite do service: 3) — vai para o RD", qtdCameras))
	}

	if c.Tipo == TipoIndefinido {
		switch {
		case qtdCameras > 0:
			c.Tipo = TipoService
			c.Motivos = append(c.Motivos,
				fmt.Sprintf("%d câmera(s), mesma tecnologia do cliente e com contrato de imagens — alteração pequena, orçamento service", qtdCameras))
		case temAlarme:
			if p.ContratoAlarme {
				c.Tipo = TipoService
				c.Motivos = append(c.Motivos, "adicionais de alarme com contrato de alarme ativo — orçamento service")
			} else {
				rd("adicionais de alarme sem contrato de alarme — ampliação, vai para o RD")
			}
		case temRast:
			c.Tipo = TipoService
			c.Motivos = append(c.Motivos, "rastreamento — avaliar com o RD se for contrato novo; com contrato ativo, orçamento service")
		default:
			c.Motivos = append(c.Motivos, "nenhum item classificável no pedido")
		}
	}
	return c
}

// contratosDoPerfil lista os contratos detectados (para exibir ao usuário).
func contratosDoPerfil(p PerfilCliente) []string {
	var out []string
	if p.ContratoAlarme {
		out = append(out, "Alarme")
	}
	if p.ContratoImagens {
		out = append(out, "Imagens (câmeras)")
	} else if p.CameraBonificada {
		out = append(out, "Alarme (câmera bonificada não conta como contrato)")
	}
	if len(out) == 0 {
		out = append(out, "nenhum contrato detectado no cadastro")
	}
	return out
}

// classificarItens separa o pedido por família, pelos nomes dos itens da
// planilha (Câmera Wifi …, DVR …, Câmera 1120/3130/…, Sensor/Controle…).
func classificarItens(s Selecao) (qtdDVR, qtdWifi int, temAlarme, temRast bool) {
	for nome, qtd := range s.Itens {
		if qtd <= 0 {
			continue
		}
		n := strings.ToUpper(nome)
		switch {
		case strings.Contains(n, "DVR") || strings.Contains(n, "CÂMERA 1120") ||
			strings.Contains(n, "CAMERA 1120") || strings.Contains(n, "CÂMERA 3130") ||
			strings.Contains(n, "CAMERA 3130") || strings.Contains(n, "CÂMERA 1220") ||
			strings.Contains(n, "CAMERA 1220") || strings.Contains(n, "CÂMERA 3240") ||
			strings.Contains(n, "CAMERA 3240") || strings.Contains(n, "CÂMERA IP") ||
			strings.Contains(n, "CAMERA IP") || strings.Contains(n, " HD"):
			qtdDVR += qtd
		case strings.Contains(n, "WIFI") || strings.Contains(n, "WI-FI") ||
			strings.Contains(n, "CÂMERA WIFI") || strings.Contains(n, "IM3") ||
			strings.Contains(n, "IM5") || strings.Contains(n, "IM7") || strings.Contains(n, "IM9"):
			qtdWifi += qtd
		case strings.Contains(n, "SENSOR") || strings.Contains(n, "CONTROLE") ||
			strings.Contains(n, "SIRENE") || strings.Contains(n, "TECLADO") ||
			strings.Contains(n, "MÓDULO") || strings.Contains(n, "CENTRAL") ||
			strings.Contains(n, "ALARME"):
			temAlarme = true
		case strings.Contains(n, "RASTREAMENTO") || strings.Contains(n, "RASTREADOR") ||
			strings.Contains(n, "IBUTTON") || strings.Contains(n, "FLEET"):
			temRast = true
		}
	}
	return qtdDVR, qtdWifi, temAlarme, temRast
}

// PerfilDosServicos deduz o perfil a partir dos serviços contratados
// (descrições do cadastro no 360) e das descrições de produtos.
func PerfilDosServicos(servicos []ServicoCliente360, descricoesProdutos []string) PerfilCliente {
	var p PerfilCliente
	for _, sv := range servicos {
		d := strings.ToUpper(sv.Descricao)
		pago := sv.Valor > 0
		switch {
		case strings.Contains(d, "IMAGEM") || strings.Contains(d, "CFTV"):
			if pago {
				p.ContratoImagens = true
			}
		case strings.Contains(d, "CÂMERA") || strings.Contains(d, "CAMERA"):
			if pago {
				p.ContratoImagens = true
			} else {
				p.CameraBonificada = true
			}
		}
		if strings.Contains(d, "ALARME") {
			p.ContratoAlarme = true
		}
	}
	for _, d := range descricoesProdutos {
		n := strings.ToUpper(d)
		if strings.Contains(n, "WIFI") || strings.Contains(n, "WI-FI") ||
			strings.Contains(n, "IM3") || strings.Contains(n, "IM5") ||
			strings.Contains(n, "IM7") || strings.Contains(n, "IM9") {
			p.TemCameraWifi = true
		}
		if strings.Contains(n, "DVR") || strings.Contains(n, "CÂMERA 1120") ||
			strings.Contains(n, "CAMERA 1120") || strings.Contains(n, "CFTV") {
			p.TemCameraDVR = true
		}
	}
	return p
}

// ServicoCliente360 é um serviço contratado do cadastro (recorte do que a
// classificação usa).
type ServicoCliente360 struct {
	Descricao  string  `json:"descricaoServico"`
	Valor      float64 `json:"valor"`
	Quantidade int     `json:"quantidade"`
}
