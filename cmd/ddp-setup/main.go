package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/DDP-Projekt/Kompilierer/cmd/internal/compression"
	"github.com/badgerodon/penv"
	"github.com/kardianos/osext"
	cp "github.com/otiai10/copy"
)

var (
	gccCmd    = "gcc"
	makeCmd   = "make"
	arCmd     = "ar"
	vscodeCmd = "code"
	kddpCmd   = "bin/kddp"
	cwd       = "./"
)

func exit(code int) {
	InfoF("Drücke die Eingabetaste, um das Fenster zu schließen...")
	if !always_yes {
		fmt.Scanln()
	}
	os.Exit(code)
}

func main() {
	flag.BoolVar(&always_yes, "force", false, "immer ja zu Aufforderungen antworten")
	flag.Parse()
	if !prompt("Willkommen beim DDP-Installer!\nDieses Setup wird einige Dateien entpacken und dich um Erlaubnis fragen, einige Umgebungsvariablen zu verändern u. Ä.\nMöchtest du fortfahren") {
		return
	}

	cwd_, err := os.Getwd()
	if err != nil {
		WarnF("Fehler beim Abrufen des aktuellen Arbeitsverzeichnisses: %s", err)
	} else {
		cwd = cwd_
	}
	installLocales()

	_, hasGcc := LookupCommand(gccCmd)

	if !hasGcc && runtime.GOOS == "windows" {
		InfoF("gcc nicht gefunden, installiere mingw64")
		InfoF("entpacke mingw64.zip")
		err := compression.DecompressFolder("mingw64.zip", "mingw64")
		if err != nil {
			ErrorF("Fehler beim Entpacken von mingw64: %s", err)
			ErrorF("gcc nicht verfügbar, Abbruch")
			exit(1)
		}
		DoneF("mingw64 entpackt")

		gccCmd, err = filepath.Abs(filepath.Join("mingw64", "bin", "gcc"))
		if err != nil {
			WarnF("Fehler beim Abrufen des absoluten Pfads: %s", err)
		}
		gccCmd = filepath.ToSlash(gccCmd)
		arCmd, err = filepath.Abs(filepath.Join("mingw64", "bin", "ar"))
		if err != nil {
			WarnF("Fehler beim Abrufen des absoluten Pfads: %s", err)
		}
		arCmd = filepath.ToSlash(arCmd)
		makeCmd, err = filepath.Abs(filepath.Join("mingw64", "bin", "mingw32-make"))
		if err != nil {
			WarnF("Fehler beim Abrufen des absoluten Pfads: %s", err)
		}
		makeCmd = filepath.ToSlash(makeCmd)

		DoneF("mingw64 installiert")
		DoneF("verwende das neu installierte mingw64 für gcc, ar und make")
	} else if !hasGcc && runtime.GOOS != "windows" {
		ErrorF("gcc nicht gefunden, Abbruch")
		exit(1)
	}

	if makeCmd == "make" { // if we don't use the zipped mingw32-make
		_, hasMake := LookupCommand(makeCmd)

		if !hasMake && runtime.GOOS == "windows" {
			InfoF("make nicht gefunden, suche nach mingw32-make")
			makeCmd, hasMake = LookupCommand("mingw32-make")
			if !hasMake {
				ErrorF("mingw32-make nicht gefunden, Abbruch")
				exit(1)
			}
			makeCmd = filepath.ToSlash(makeCmd)
		} else if !hasMake && runtime.GOOS != "windows" {
			WarnF("make nicht gefunden")
		}
	}

	if isSameGccVersion() {
		DoneF("gcc-Versionen stimmen überein")
	} else {
		InfoF("kompiliere runtime und stdlib neu")
		recompileLibs()
	}

	if vscodeCmd, hasVscode := LookupCommand(vscodeCmd); hasVscode && prompt("Möchtest du vscode-ddp (die DDP vscode-Erweiterung) installieren") {
		InfoF("installiere vscode-ddp als vscode-Erweiterung")
		if _, err := runCmd("", vscodeCmd, "--install-extension", "DDP-Projekt.vscode-ddp", "--force"); err == nil {
			DoneF("vscode-ddp installiert")
		}
	}

	if prompt("Möchtest du die Umgebungsvariable DDPPATH setzen") {
		if exedir, err := osext.ExecutableFolder(); err != nil {
			WarnF("Ausführungspfad konnte nicht abgerufen werden")
		} else {
			InfoF("Setze die Umgebungsvariable DDPPATH auf %s", exedir)
			if err := penv.SetEnv("DDPPATH", exedir); err != nil {
				ErrorF("Fehler beim Setzen von DDPPATH: %s\nVersuche, es selbst hinzuzufügen", err)
			}
		}
	}

	if prompt("Möchtest du das Verzeichnis DDP/bin zu PATH hinzufügen") {
		if exedir, err := osext.ExecutableFolder(); err != nil {
			WarnF("Ausführungspfad konnte nicht abgerufen werden")
		} else {
			binPath := filepath.Join(exedir, "bin")
			InfoF("Füge %s zum PATH hinzu", binPath)
			if err := penv.AppendEnv("PATH", binPath); err != nil {
				ErrorF("Fehler beim Hinzufügen zu PATH: %s\nVersuche, DDP/bin selbst zu PATH hinzuzufügen", err)
			}
		}
	}

	if !errored {
		DoneF("DDP ist jetzt installiert")
		if prompt("Möchtest du Dateien löschen, die nicht mehr benötigt werden") {
			if runtime.GOOS == "windows" {
				InfoF("lösche mingw64.zip")
				if err := os.Remove("mingw64.zip"); err != nil {
					WarnF("Fehler beim Entfernen von mingw64.zip: %s", err)
				} else {
					DoneF("mingw64.zip entfernt")
				}
			}
		}
		DoneF("Die DDP-Installation wurde erfolgreich abgeschlossen, du kannst sie jetzt löschen")
	}
	exit(0)
}

func installLocales() {
	InfoF("installiere deutsche Lokalisierung")
	if runtime.GOOS == "linux" {
		if _, err := runCmd("", "locale-gen", "de_DE.UTF-8"); err != nil {
			WarnF("Fehler bei der Installation der deutschen Lokalisierung: %s", err)
		}
	} else if runtime.GOOS == "windows" {
		WarnF("du verwendest Windows, stell sicher, dass du die richtigen Sprachpakete installiert hast")
	}
}

func isSameGccVersion() bool {
	gccVersion, err := runCmd("", gccCmd, "-dumpfullversion")
	if err != nil {
		return false
	}
	gccVersion = strings.Trim(gccVersion, "\r\n") // TODO: this
	kddpVersionOutput, err := runCmd("", filepath.Join("bin", "kddp"), "version", "--wortreich")
	if err != nil {
		return false
	}
	gccVersionLine := strings.Split(kddpVersionOutput, "\n")[2]
	kddpGccVersion := strings.Trim(strings.Split(gccVersionLine, " ")[2], "\r\n")
	match := gccVersion == kddpGccVersion
	if !match {
		InfoF("lokale gcc-Version und kddp gcc-Version stimmen nicht überein (%s vs %s)", gccVersion, kddpGccVersion)
	}
	return match
}

func recompileLibs() {
	make_args := make([]string, 0)
	rmArg := ""
	if runtime.GOOS == "windows" {
		make_args = append(make_args, fmt.Sprintf("CC=%s", gccCmd), fmt.Sprintf("AR=%s %s", arCmd, "rcs"))
		rmArg = fmt.Sprintf("%s %s", filepath.Join(cwd, "bin", "kddp.exe"), "rm")
	}

	if _, err := runCmd("lib/runtime/", makeCmd, make_args...); err != nil {
		return
	}
	DoneF("runtime neu kompiliert")
	if _, err := runCmd("lib/stdlib/", makeCmd, make_args...); err != nil {
		return
	}
	DoneF("stdlib neu kompiliert")

	InfoF("entferne vor-kompilierte runtime")
	if err := os.Remove("lib/libddpruntime.a"); err != nil {
		WarnF("Fehler beim Entfernen der vor-kompilierten runtime: %s", err)
	}
	InfoF("entferne vor-kompilierte lib/main.o lib/ddp_list_types_defs.o lib/ddp_list_types_defs.ll")
	if err := os.Remove("lib/main.o"); err != nil {
		WarnF("Fehler beim Entfernen der vor-kompilierten lib/main.o: %s", err)
	}
	if err := os.Remove("lib/ddp_list_types_defs.o"); err != nil {
		WarnF("Fehler beim Entfernen der vor-kompilierten lib/ddp_list_types_defs.o: %s", err)
	}
	if err := os.Remove("lib/ddp_list_types_defs.ll"); err != nil {
		WarnF("Fehler beim Entfernen der vor-kompilierten lib/ddp_list_types_defs.ll: %s", err)
	}
	InfoF("entferne vor-kompiliertes stdlib")
	if err := os.Remove("lib/libddpstdlib.a"); err != nil {
		WarnF("Fehler beim Entfernen des vor-kompilierten stdlibs: %s", err)
	}

	InfoF("kopiere neu kompilierte runtime")
	if err := cp.Copy("lib/runtime/libddpruntime.a", "lib/libddpruntime.a"); err != nil {
		ErrorF("Fehler beim Kopieren der neu kompilierten runtime: %s", err)
	}
	InfoF("kopiere neu kompilierte lib/main.o")
	if err := cp.Copy("lib/runtime/source/main.o", "lib/main.o"); err != nil {
		ErrorF("Fehler beim Kopieren der neu kompilierten runtime: %s", err)
	}
	InfoF("erstelle lib/ddp_list_types_defs.ll und lib/ddp_list_types_defs.o neu")
	if _, err := runCmd("", kddpCmd, "dump-list-defs", "-o", "lib/ddp_list_types_defs", "--llvm_ir", "--object"); err != nil {
		ErrorF("Fehler bei der Neuerstellung von lib/ddp_list_types_defs.ll und lib/ddp_list_types_defs.o: %s", err)
	}
	InfoF("kopiere neu kompiliertes stdlib")
	if err := cp.Copy("lib/stdlib/libddpstdlib.a", "lib/libddpstdlib.a"); err != nil {
		ErrorF("Fehler beim Kopieren des neu kompilierten stdlibs: %s", err)
	}

	InfoF("säubere das Verzeichnis runtime")
	clean_args := make([]string, 0, 2)
	clean_args = append(clean_args, "clean")
	if rmArg != "" {
		clean_args = append(clean_args, rmArg)
	}
	if _, err := runCmd("lib/runtime/", makeCmd, clean_args...); err != nil {
		WarnF("Fehler beim Säubern des runtime-Verzeichnisses: %s", err)
	}
	InfoF("säubere das Verzeichnis stdlib")
	if _, err := runCmd("lib/stdlib/", makeCmd, clean_args...); err != nil {
		WarnF("Fehler beim Säubern des stdlib-Verzeichnisses: %s", err)
	}

	DoneF("Bibliotheken neu kompiliert")
}

func runCmd(dir string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmdStr := cmd.String()
	InfoF(cmdStr)
	out, err := cmd.CombinedOutput()
	if err != nil {
		ErrorF("'%s' fehlgeschlagen (%s) Ausgabe: %s", cmdStr, err, out)
	}
	return string(out), err
}

func LookupCommand(cmd string) (string, bool) {
	InfoF("Suche nach %s", cmd)
	path, err := exec.LookPath(cmd)
	if err == nil {
		DoneF("Gefunden %s in %s", cmd, path)
	} else {
		WarnF("Konnte %s nicht finden", cmd)
	}
	return path, err == nil
}
