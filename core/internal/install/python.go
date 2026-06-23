package install

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// isWindowsStorePythonStub reports whether path is a Microsoft Store
// "App Execution Alias" for Python rather than a real interpreter. These stubs
// live under ...\AppData\Local\Microsoft\WindowsApps\ and, when executed
// without the Store package installed, just print "Python was not found..."
// and exit with code 9009. Treating them as a real Python yields a misleading
// "ensurepip indisponível" failure, so we skip them outright.
func isWindowsStorePythonStub(path string) bool {
	if runtime.GOOS != "windows" {
		return false
	}
	return strings.Contains(strings.ToLower(path), `\microsoft\windowsapps\`)
}

// findCompatiblePython localiza um interpretador Python compatível com
// headroom-ai. Versões 3.10–3.12 têm wheels disponíveis para todas as
// dependências; 3.13+ frequentemente quebram (faltam wheels para libs com
// extensões C). Em macOS isso costuma se manifestar como "no pip in venv"
// quando o Homebrew default pula pra uma versão muito nova.
//
// Antes de retornar, valida que o interpretador tem `ensurepip` funcional e
// que `xml.parsers.expat` carrega — em macOS+Homebrew o pyexpat às vezes
// fica linkado ao libexpat do sistema e quebra o pip silenciosamente.
//
// Quando NENHUM Python é encontrado, tenta uma instalação automática
// best-effort (winget/brew/gerenciador de pacotes) e refaz a busca. Se a
// instalação não for possível, retorna o erro original com a dica de
// remediação manual — nunca trava em prompts.
func findCompatiblePython() (string, error) {
	path, anyFound, err := lookupCompatiblePython()
	if err == nil {
		return path, nil
	}
	// Só tenta instalar quando nada foi achado. Se um Python existe mas falhou
	// no pre-flight (ex.: pyexpat quebrado), instalar outro provavelmente não
	// resolve e a dica de remediação é mais útil que uma reinstalação.
	if !anyFound {
		if instErr := tryInstallPython(); instErr != nil {
			fmt.Printf("  ⚠ headroom: instalação automática do Python falhou (%v)\n", instErr)
		} else if path, _, reErr := lookupCompatiblePython(); reErr == nil {
			return path, nil
		}
	}
	return "", err
}

// lookupCompatiblePython percorre os candidatos e devolve o primeiro
// interpretador que passa no pre-flight. anyFound indica se ao menos um Python
// real (não stub da Store) foi localizado, mesmo que tenha reprovado — isso
// permite ao chamador decidir entre tentar instalar (nada achado) ou apenas
// orientar o usuário (achado mas inválido).
func lookupCompatiblePython() (string, bool, error) {
	candidates := []string{"python3.12", "python3.11", "python3.10", "python3", "python"}
	if runtime.GOOS == "windows" {
		// Windows rarely has versioned python3.x on PATH; the "py" launcher is
		// the most reliable real interpreter, while "python"/"python3" on PATH
		// are frequently the Microsoft Store alias stubs.
		candidates = []string{"py", "python", "python3"}
	}
	var lastErr error
	var anyFound bool
	for _, name := range candidates {
		path, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		if isWindowsStorePythonStub(path) {
			// Not a real interpreter — the Store alias stub. Skip it without
			// marking anyFound, so the user gets the "install Python" hint.
			fmt.Printf("  ⚠ headroom: ignorando alias da Microsoft Store %s (não é um Python real)\n", path)
			continue
		}
		anyFound = true
		warnIfNewerPython(path)
		if err := validatePython(path); err != nil {
			lastErr = fmt.Errorf("%s: %w", path, err)
			fmt.Printf("  ⚠ headroom: pulando %s (%v)\n", path, err)
			continue
		}
		return path, true, nil
	}
	if anyFound && lastErr != nil {
		return "", true, fmt.Errorf("nenhum Python encontrado passou no pre-flight: %w\n%s",
			lastErr, pythonRemediationHint())
	}
	return "", false, fmt.Errorf("python não encontrado no PATH (instale Python 3.10–3.12)\n%s", pythonRemediationHint())
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
	case "windows":
		return "  Windows: instale o Python 3.10–3.12 de https://python.org (marque \"Add python.exe to PATH\")\n" +
			"  ou via winget: winget install Python.Python.3.12"
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
