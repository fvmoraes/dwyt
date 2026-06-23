package install

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// httpClient é o client HTTP compartilhado pelos installers. Todos os
// downloads (scripts curtos do install.sh upstream, DMGs de centenas de MB,
// AppImages do Obsidian) precisam de um teto. Sem timeout, uma rede ruim
// segura o install indefinidamente e o wizard fica preso em "installing".
var httpClient = &http.Client{Timeout: 5 * time.Minute}

// fetch baixa o corpo de uma URL como string. Vazio se falhar — chamadores
// devem checar se a string é vazia antes de seguir. Recusa esquemas que não
// sejam HTTPS: scripts de install são canalizados para o shell, então um
// downgrade para HTTP (MITM) seria um vetor de execução de código.
func fetch(url string) string {
	if !strings.HasPrefix(strings.ToLower(url), "https://") {
		return ""
	}
	resp, err := httpClient.Get(url)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	body, _ := io.ReadAll(resp.Body)
	return string(body)
}

// copyExecutable copia src para dst preservando permissão executável e
// criando os diretórios pai necessários.
//
// A escrita é atômica: o conteúdo vai primeiro para um arquivo temporário no
// mesmo diretório e só então é renomeado por cima do destino. Isso evita o
// ETXTBSY ("text file busy") do Linux ao reinstalar um binário que está em
// execução (ex.: o dwyt-obsidian-mcp rodando) — o rename troca a entrada do
// diretório sem abrir/truncar o inode em uso pelo processo ativo.
func copyExecutable(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(dst), ".dwyt-"+filepath.Base(dst)+"-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// Em qualquer caminho de erro, não deixa o temporário órfão.
	defer os.Remove(tmpName)

	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0755); err != nil {
		return err
	}
	// Rename atômico no mesmo diretório: substitui o destino sem tocar no inode
	// que um processo em execução possa estar mapeando.
	return os.Rename(tmpName, dst)
}

// sameFile retorna true quando dois paths apontam para o mesmo arquivo após
// resolução de paths absolutos. Usado para evitar self-copy.
func sameFile(a, b string) bool {
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return false
	}
	return aa == bb
}

// canWriteDir testa se o processo atual pode criar arquivos em dir, sem
// efeitos colaterais persistentes.
func canWriteDir(dir string) bool {
	probe := filepath.Join(dir, ".dwyt-write-probe")
	f, err := os.Create(probe)
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(probe)
	return true
}
