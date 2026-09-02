package install

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// pythonCommand is an interpreter together with arguments that must precede
// the regular Python arguments.  Windows' py launcher needs this shape: the
// requested interpreter version (for example -3.12) is an argument to py,
// not part of the executable name.
type pythonCommand struct {
	bin        string
	prefixArgs []string
}

func (p pythonCommand) String() string {
	return strings.TrimSpace(strings.Join(append([]string{p.bin}, p.prefixArgs...), " "))
}

func (p pythonCommand) commandArgs(args ...string) []string {
	full := make([]string, 0, len(p.prefixArgs)+len(args))
	full = append(full, p.prefixArgs...)
	return append(full, args...)
}

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
// Quando nenhum Python compatível é encontrado, tenta uma instalação
// automática best-effort (winget/brew/gerenciador de pacotes) e refaz a
// busca. Se a instalação não for possível, retorna o erro original com a dica
// de remediação manual — nunca trava em prompts.
func findCompatiblePython() (string, error) {
	python, err := findCompatiblePythonCommand()
	if err != nil {
		return "", err
	}
	return python.bin, nil
}

// findCompatiblePythonCommand is the command-aware counterpart to
// findCompatiblePython.  Headroom uses it so Windows can invoke the py
// launcher as `py -3.12`, rather than accidentally accepting py's default
// interpreter (which may be Python 3.13+).
func findCompatiblePythonCommand() (pythonCommand, error) {
	python, anyCompatible, err := lookupCompatiblePythonCommand()
	if err == nil {
		return python, nil
	}
	// Só evita reinstalar quando uma versão compatível já foi localizada, mas
	// falhou no pre-flight (ex.: pyexpat quebrado). Um Python 3.13+ instalado
	// não é compatível para este bootstrap e deve permitir a tentativa de
	// instalar o 3.12 automaticamente.
	if !anyCompatible {
		if instErr := tryInstallPython(); instErr != nil {
			fmt.Printf("  ⚠ headroom: instalação automática do Python falhou (%v)\n", instErr)
		} else if python, _, reErr := lookupCompatiblePythonCommand(); reErr == nil {
			return python, nil
		} else {
			err = reErr
		}
	}
	return pythonCommand{}, err
}

// lookupCompatiblePython preserves the older string-returning helper for
// callers that only need the executable path. Headroom itself must use
// lookupCompatiblePythonCommand to retain py's version selector.
func lookupCompatiblePython() (string, bool, error) {
	python, compatible, err := lookupCompatiblePythonCommand()
	return python.bin, compatible, err
}

// lookupCompatiblePythonCommand percorre os candidatos e devolve o primeiro
// interpretador 3.10–3.12 que passa no pre-flight. anyCompatible indica se
// uma versão suportada foi localizada, mesmo que seu pre-flight falhe; isso
// evita reinstalar um Python 3.12 local que precisa de reparo. Interpreters
// 3.13+ não contam como compatíveis, para que o bootstrap possa instalar 3.12.
func lookupCompatiblePythonCommand() (pythonCommand, bool, error) {
	return lookupCompatiblePythonCandidates(
		pythonCandidatesForOS(runtime.GOOS),
		exec.LookPath,
		isWindowsStorePythonStub,
		pythonCommandMajorMinor,
		validatePythonCommand,
	)
}

func pythonCandidatesForOS(goos string) []pythonCommand {
	if goos == "windows" {
		// py is the most reliable launcher on Windows, but it must be told to
		// select 3.12. Invoking bare `py` silently chooses the user's default
		// Python, which is commonly 3.13+ after a system upgrade.
		return []pythonCommand{
			{bin: "py", prefixArgs: []string{"-3.12"}},
			{bin: "python3.12"},
			{bin: "python"},
			{bin: "python3"},
		}
	}
	return []pythonCommand{
		{bin: "python3.12"},
		{bin: "python3.11"},
		{bin: "python3.10"},
		{bin: "python3"},
		{bin: "python"},
	}
}

// lookupCompatiblePythonCandidates contains the decision logic separately
// from process discovery so the Windows launcher selection can be tested
// without depending on the machine's installed Pythons.
func lookupCompatiblePythonCandidates(
	candidates []pythonCommand,
	lookPath func(string) (string, error),
	isStoreStub func(string) bool,
	version func(pythonCommand) (int, int, bool),
	validate func(pythonCommand) error,
) (pythonCommand, bool, error) {
	var lastErr error
	var anyCompatible bool
	for _, candidate := range candidates {
		path, err := lookPath(candidate.bin)
		if err != nil {
			continue
		}
		if isStoreStub(path) {
			// Not a real interpreter — the Store alias stub. Skip it without
			// marking anyCompatible, so the automatic 3.12 install can run.
			fmt.Printf("  ⚠ headroom: ignorando alias da Microsoft Store %s (não é um Python real)\n", path)
			continue
		}
		candidate.bin = path
		maj, min, ok := version(candidate)
		if !ok {
			lastErr = fmt.Errorf("%s: não foi possível determinar a versão do Python", candidate)
			fmt.Printf("  ⚠ headroom: pulando %s (%v)\n", candidate, lastErr)
			continue
		}
		if err := validatePythonVersion(maj, min); err != nil {
			lastErr = fmt.Errorf("%s: %w", candidate, err)
			fmt.Printf("  ⚠ headroom: pulando %s (%v)\n", candidate, err)
			continue
		}
		anyCompatible = true
		if err := validate(candidate); err != nil {
			lastErr = fmt.Errorf("%s: %w", candidate, err)
			fmt.Printf("  ⚠ headroom: pulando %s (%v)\n", candidate, err)
			continue
		}
		return candidate, true, nil
	}
	if lastErr != nil {
		return pythonCommand{}, anyCompatible, fmt.Errorf("nenhum Python compatível encontrado passou no pre-flight: %w\n%s",
			lastErr, pythonRemediationHint())
	}
	return pythonCommand{}, false, fmt.Errorf("python 3.10–3.12 não encontrado no PATH\n%s", pythonRemediationHint())
}

func validatePythonVersion(major, minor int) error {
	if major != 3 || minor < 10 || minor > 12 {
		return fmt.Errorf("Python %d.%d não suportado para Headroom (requer 3.10–3.12)", major, minor)
	}
	return nil
}

// validatePython garante que o interpretador tem ensurepip e que pyexpat
// carrega corretamente. Sem isso o `python -m venv` cria um venv quebrado
// que aparece muito depois, no `pip install`.
func validatePython(bin string) error {
	return validatePythonCommand(pythonCommand{bin: bin})
}

func validatePythonCommand(python pythonCommand) error {
	if out, err := exec.Command(python.bin, python.commandArgs("-m", "ensurepip", "--version")...).CombinedOutput(); err != nil {
		return fmt.Errorf("ensurepip indisponível: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	if out, err := exec.Command(python.bin, python.commandArgs("-c", "from xml.parsers import expat")...).CombinedOutput(); err != nil {
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
	return pythonCommandMajorMinor(pythonCommand{bin: bin})
}

func pythonCommandMajorMinor(python pythonCommand) (int, int, bool) {
	out, err := exec.Command(python.bin, python.commandArgs("-c", "import sys;print(sys.version_info[0],sys.version_info[1])")...).Output()
	if err != nil {
		return 0, 0, false
	}
	var maj, min int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(out)), "%d %d", &maj, &min); err != nil {
		return 0, 0, false
	}
	return maj, min, true
}
