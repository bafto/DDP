package ddppath

import (
	"os"
	"path/filepath"

	"github.com/kardianos/osext"
)

var (
	// path to the directory where DDP is installed
	InstallDir string
	// path to the directory of the Duden
	Duden string
	// path to the DDP/bin dir (contains kddp.exe and probably DDPLS.exe)
	Bin string
	// path to the DDP/lib dir (contains ddpstdlib.a and ddpruntime.a and probably their sources)
	Lib string
	// path to the mingw64 directory in the DDP installation directory
	// might not be present
	Mingw64 string
)

func init() {
	// get the path to the ddp install directory
	if ddppath := os.Getenv("DDPPATH"); ddppath != "" {
		InstallDir = ddppath
	} else if exeFolder, err := osext.ExecutableFolder(); err != nil { // fallback if the environment variable is not set, might fail though
		panic(err)
	} else {
		InstallDir, err = filepath.Abs(filepath.Join(exeFolder, "../"))
		if err != nil {
			panic(err)
		}
	}
	Duden = filepath.Join(InstallDir, "Duden")
	Bin = filepath.Join(InstallDir, "bin")
	Lib = filepath.Join(InstallDir, "lib")
	Mingw64 = filepath.Join(InstallDir, "mingw64")
}
