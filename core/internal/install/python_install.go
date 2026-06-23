package install

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// pythonInstallTimeout caps each automated Python install attempt. Cold-network
// brew builds and winget downloads can be slow, so the budget is generous while
// still protecting the wizard from hanging forever.
const pythonInstallTimeout = 20 * time.Minute

// tryInstallPython faz uma instalação best-effort e não-interativa de um Python
// compatível (3.12) para o SO atual e, em caso de sucesso, deixa o binário
// visível no PATH do processo corrente. É acionada apenas quando nenhum Python
// foi encontrado. Qualquer falha é retornada para que o chamador caia na dica
// de instalação manual — esta função nunca bloqueia em prompts interativos.
func tryInstallPython() error {
	switch runtime.GOOS {
	case "windows":
		return installPythonWindows()
	case "darwin":
		return installPythonMacOS()
	case "linux":
		return installPythonLinux()
	default:
		return fmt.Errorf("instalação automática de Python não suportada em %s", runtime.GOOS)
	}
}

// installPythonWindows usa o winget (mesmo mecanismo já usado para a toolchain
// de build) para instalar o Python 3.12 e então procura o diretório de
// instalação para expô-lo no PATH do processo, já que o winget só atualiza o
// PATH de novas sessões.
func installPythonWindows() error {
	if _, err := exec.LookPath("winget"); err != nil {
		return fmt.Errorf("winget indisponível para instalar o Python automaticamente")
	}
	fmt.Println("  → headroom: instalando Python 3.12 via winget…")
	if err := runWingetInstall("Python.Python.3.12"); err != nil {
		return err
	}
	ensurePythonOnPathWindows()
	return nil
}

// ensurePythonOnPathWindows varre os locais padrão onde o winget instala o
// Python (per-user em LOCALAPPDATA e all-users em Program Files) e prepende os
// diretórios com python.exe ao PATH do processo atual.
func ensurePythonOnPathWindows() {
	var bases []string
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		bases = append(bases, filepath.Join(local, "Programs", "Python"))
	}
	for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)"} {
		if base := os.Getenv(env); base != "" {
			bases = append(bases, base)
		}
	}
	for _, base := range bases {
		entries, err := os.ReadDir(base)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() || !strings.HasPrefix(strings.ToLower(e.Name()), "python3") {
				continue
			}
			pdir := filepath.Join(base, e.Name())
			if _, err := os.Stat(filepath.Join(pdir, "python.exe")); err == nil {
				prependPath(pdir)
				prependPath(filepath.Join(pdir, "Scripts"))
			}
		}
	}
}

// installPythonMacOS instala o python@3.12 via Homebrew quando o brew está
// disponível. O brew é pré-requisito por design: instalar o próprio Homebrew
// seria invasivo demais para um passo automático. Sem brew, retorna erro e o
// chamador cai na dica manual.
func installPythonMacOS() error {
	brew, err := exec.LookPath("brew")
	if err != nil {
		return fmt.Errorf("homebrew (brew) não encontrado; instale o Python 3.10–3.12 manualmente")
	}
	fmt.Println("  → headroom: instalando Python 3.12 via Homebrew…")
	if err := runWithTimeout(pythonInstallTimeout, brew, "install", "python@3.12"); err != nil {
		return err
	}
	ensurePythonOnPathMacOS(brew)
	return nil
}

// ensurePythonOnPathMacOS resolve o prefixo do formula keg-only python@3.12 e
// prepende seu bin (que expõe python3.12) ao PATH do processo, já que o brew
// não linka formulas keg-only no PATH automaticamente.
func ensurePythonOnPathMacOS(brew string) {
	out, err := exec.Command(brew, "--prefix", "python@3.12").Output()
	if err != nil {
		return
	}
	prefix := strings.TrimSpace(string(out))
	if prefix == "" {
		return
	}
	prependPath(filepath.Join(prefix, "bin"))
	prependPath(filepath.Join(prefix, "libexec", "bin"))
}

// installPythonLinux detecta o gerenciador de pacotes da distro e instala o
// Python + venv + libexpat de forma não-interativa. Como isso normalmente exige
// root, usa `sudo -n` quando o processo não é root: se não houver sudo sem
// senha, o comando falha de imediato (em vez de travar pedindo senha) e o
// chamador cai na dica manual.
func installPythonLinux() error {
	mgr, cmds := linuxPythonInstallCmds()
	if mgr == "" {
		return fmt.Errorf("nenhum gerenciador de pacotes suportado (apt/dnf/pacman/zypper/apk) encontrado")
	}
	fmt.Printf("  → headroom: instalando Python via %s (pode exigir sudo sem senha)…\n", mgr)
	return runLinuxPkgInstall(mgr, cmds)
}

// linuxPythonInstallCmds devolve o nome do gerenciador detectado e a sequência
// de comandos para instalar o Python 3 nele. Retorna "" quando nenhum
// gerenciador conhecido está no PATH.
func linuxPythonInstallCmds() (string, [][]string) {
	type pm struct {
		bin  string
		cmds [][]string
	}
	managers := []pm{
		{"apt-get", [][]string{
			{"apt-get", "update"},
			{"apt-get", "install", "-y", "python3", "python3-venv", "python3-pip", "libexpat1"},
		}},
		{"dnf", [][]string{
			{"dnf", "install", "-y", "python3", "python3-pip", "expat"},
		}},
		{"pacman", [][]string{
			{"pacman", "-Sy", "--noconfirm", "python", "python-pip", "expat"},
		}},
		{"zypper", [][]string{
			{"zypper", "--non-interactive", "install", "python3", "python3-pip", "libexpat1"},
		}},
		{"apk", [][]string{
			{"apk", "add", "python3", "py3-pip", "expat"},
		}},
	}
	for _, m := range managers {
		if _, err := exec.LookPath(m.bin); err == nil {
			return m.bin, m.cmds
		}
	}
	return "", nil
}

// runLinuxPkgInstall executa cada comando do gerenciador, prefixando com
// `sudo -n` quando o processo não roda como root. `sudo -n` falha rápido se
// exigir senha, mantendo a instalação silenciosa.
func runLinuxPkgInstall(mgr string, cmds [][]string) error {
	needSudo := os.Geteuid() != 0
	if needSudo {
		if _, err := exec.LookPath("sudo"); err != nil {
			return fmt.Errorf("%s requer privilégios de root e o sudo não está disponível", mgr)
		}
	}
	for _, c := range cmds {
		full := c
		if needSudo {
			full = append([]string{"sudo", "-n"}, c...)
		}
		if err := runWithTimeout(pythonInstallTimeout, full[0], full[1:]...); err != nil {
			if needSudo {
				return fmt.Errorf("%w\n  (sudo sem senha indisponível? rode manualmente: sudo %s)",
					err, strings.Join(c, " "))
			}
			return err
		}
	}
	return nil
}

// runWithTimeout roda um comando com saída/erro encaminhados ao usuário e um
// teto de tempo, transformando o estouro em erro legível em vez de travar.
func runWithTimeout(timeout time.Duration, bin string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("%s expirou após %s", bin, timeout)
		}
		return err
	}
	return nil
}

// prependPath adiciona dir ao início do PATH do processo atual quando ele
// existe e ainda não está presente. Permite que uma instalação recém-feita seja
// resolvida sem esperar uma nova sessão de shell.
func prependPath(dir string) {
	if dir == "" {
		return
	}
	if _, err := os.Stat(dir); err != nil {
		return
	}
	path := os.Getenv("PATH")
	if strings.Contains(strings.ToLower(path), strings.ToLower(dir)) {
		return
	}
	os.Setenv("PATH", dir+string(os.PathListSeparator)+path)
}
