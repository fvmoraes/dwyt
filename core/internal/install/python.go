package install

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// findCompatiblePython localiza um interpretador Python compatível com
// headroom-ai. Versões 3.10–3.12 têm wheels disponíveis para todas as
// dependências; 3.13+ frequentemente quebram (faltam wheels para libs com
// extensões C). Em macOS isso costuma se manifestar como "no pip in venv"
// quando o Homebrew default pula pra uma versão muito nova.
//
// Antes de retornar, valida que o interpretador tem `ensurepip` funcional e
// que `xml.parsers.expat` carrega — em macOS+Homebrew o pyexpat às vezes
// fica linkado ao libexpat do sistema e quebra o pip silenciosamente.
func findCompatiblePython() (string, error) {
	candidates := []string{"python3.12", "python3.11", "python3.10", "python3", "python"}
	var lastErr error
	var anyFound bool
	for _, name := range candidates {
		path, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		anyFound = true
		warnIfNewerPython(path)
		if err := validatePython(path); err != nil {
			lastErr = fmt.Errorf("%s: %w", path, err)
			fmt.Printf("  ⚠ headroom: pulando %s (%v)\n", path, err)
			continue
		}
		return path, nil
	}
	if anyFound && lastErr != nil {
		return "", fmt.Errorf("nenhum Python encontrado passou no pre-flight: %w\n%s",
			lastErr, pythonRemediationHint())
	}
	return "", fmt.Errorf("python3 não encontrado no PATH (instale Python 3.10–3.12; macOS: brew install python@3.12)")
}

// warnIfNewerPython emite um aviso para Python 3.13+, mas segue tentando —
// algumas dependências do headroom têm wheels pra 3.13 enquanto outras não.
func warnIfNewerPython(path string) {
	maj, min, ok := pythonMajorMinor(path)
	if !ok {
		return
	}
	if maj > 3 || (maj == 3 && min >= 13) {
		fmt.Printf("  ⚠ headroom: %s reportou Python %d.%d — pode não ter wheels para todas as dependências; recomendado 3.10–3.12\n", path, maj, min)
	}
}

// validatePython garante que o interpretador tem ensurepip e que pyexpat
// carrega corretamente. Sem isso o `python -m venv` cria um venv quebrado
// que aparece muito depois, no `pip install`.
func validatePython(bin string) error {
	if out, err := exec.Command(bin, "-m", "ensurepip", "--version").CombinedOutput(); err != nil {
		return fmt.Errorf("ensurepip indisponível: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	if out, err := exec.Command(bin, "-c", "from xml.parsers import expat").CombinedOutput(); err != nil {
		return fmt.Errorf("pyexpat quebrado (provável dessincronia libexpat ↔ Python): %w\n%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func pythonRemediationHint() string {
	switch runtime.GOOS {
	case "darwin":
		return "  Tente: brew reinstall python@3.12 expat\n" +
			"  Se persistir (pyexpat dessincronizado), aponte o pyexpat.so pro libexpat do Homebrew:\n" +
			"    install_name_tool -change /usr/lib/libexpat.1.dylib \\\n" +
			"      /opt/homebrew/opt/expat/lib/libexpat.1.dylib \\\n" +
			"      $(python3.12 -c 'import pyexpat,os;print(pyexpat.__file__)')\n" +
			"    codesign --force --sign - <pyexpat-path-acima>"
	case "linux":
		return "  Tente: instale o pacote dev do Python (ex: apt install python3.12-venv) e libexpat1"
	default:
		return "  Reinstale o Python 3.10–3.12"
	}
}

func pythonMajorMinor(bin string) (int, int, bool) {
	out, err := exec.Command(bin, "-c", "import sys;print(sys.version_info[0],sys.version_info[1])").Output()
	if err != nil {
		return 0, 0, false
	}
	var maj, min int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(out)), "%d %d", &maj, &min); err != nil {
		return 0, 0, false
	}
	return maj, min, true
}
