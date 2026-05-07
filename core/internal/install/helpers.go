package install

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// fetch baixa o corpo de uma URL como string. Vazio se falhar — chamadores
// devem checar se a string é vazia antes de seguir.
func fetch(url string) string {
	resp, err := http.Get(url)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return string(body)
}

// copyExecutable copia src para dst preservando permissão executável e
// criando os diretórios pai necessários.
func copyExecutable(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
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
