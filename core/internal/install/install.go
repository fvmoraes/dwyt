// Package install bootstrap das dependências do DWYT (codebase-memory-mcp,
// rtk, headroom, obsidian-mcp e o Obsidian desktop).
//
// Cada ferramenta tem seu próprio arquivo (cbmcp.go, rtk.go, headroom.go,
// obsidian_*.go). Este arquivo intencionalmente fica vazio: o ponto de
// entrada de cada install é a função pública exportada no respectivo
// arquivo. Helpers compartilhados estão em helpers.go.
package install
