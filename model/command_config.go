package model

import (
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/revel/cmd"
	"github.com/revel/cmd/model/command"
	"github.com/revel/cmd/utils"
	"golang.org/x/mod/modfile"
)

// The constants
const (
	NEW COMMAND = iota + 1
	RUN
	BUILD
	PACKAGE
	CLEAN
	TEST
	VERSION
)

type (
	// The Revel command type
	COMMAND int

	// The Command config for the line input
	CommandConfig struct {
		Index            COMMAND  // The index
		Verbose          []bool   `short:"v" long:"debug" description:"If set the logger is set to verbose"` // True if debug is active
		FrameworkVersion *Version // The framework version
		CommandVersion   *Version // The command version
		HistoricMode     bool     `long:"historic-run-mode" description:"If set the runmode is passed a string not json"` // True if debug is active
		ImportPath       string   // The import path (relative to a GOPATH)
		// GoPath           string   // The GoPath
		GoCmd string // The full path to the go executable
		//SrcRoot           string                                                                                                                      // The source root
		AppPath           string                     // The application path (absolute)
		AppName           string                     // The application name
		HistoricBuildMode bool                       `long:"historic-build-mode" description:"If set the code is scanned using the original parsers, not the go.1.11+"` // True if debug is active
		Vendored          bool                       // True if the application is vendored
		PackageResolver   func(pkgName string) error //  a package resolver for the config
		BuildFlags        []string                   `short:"X" long:"build-flags" description:"These flags will be used when building the application. May be specified multiple times, only applicable for Build, Run, Package, Test commands"`
		GoModFlags        []string                   `long:"gomod-flags" description:"These flags will execute go mod commands for each flag, this happens during the build process"`
		New               command.New                `command:"new"`
		Build             command.Build              `command:"build"`
		Run               command.Run                `command:"run"`
		Package           command.Package            `command:"package"`
		Clean             command.Clean              `command:"clean"`
		Test              command.Test               `command:"test"`
		Version           command.Version            `command:"version"`
	}
)

// Updates the import path depending on the command
func (c *CommandConfig) UpdateImportPath() error {
	var importPath string
	required := true
	switch c.Index {
	case NEW:
		importPath = c.New.ImportPath
	case RUN:
		importPath = c.Run.ImportPath
		c.Vendored = utils.Exists(filepath.Join(importPath, "go.mod"))
	case BUILD:
		importPath = c.Build.ImportPath
		c.Vendored = utils.Exists(filepath.Join(importPath, "go.mod"))
	case PACKAGE:
		importPath = c.Package.ImportPath
		c.Vendored = utils.Exists(filepath.Join(importPath, "go.mod"))
	case CLEAN:
		importPath = c.Clean.ImportPath
		c.Vendored = utils.Exists(filepath.Join(importPath, "go.mod"))
	case TEST:
		importPath = c.Test.ImportPath
		c.Vendored = utils.Exists(filepath.Join(importPath, "go.mod"))
	case VERSION:
		importPath = c.Version.ImportPath
		required = false
	}

	if len(importPath) == 0 || filepath.IsAbs(importPath) || importPath[0] == '.' {
		utils.Logger.Info("Import path is absolute or not specified", "path", importPath)
		// Try to determine the import path from the GO paths and the command line
		currentPath, err := os.Getwd()
		if len(importPath) > 0 {
			if importPath[0] == '.' {
				// For a relative path
				importPath = filepath.Join(currentPath, importPath)
			}
			// For an absolute path
			currentPath, _ = filepath.Abs(importPath)
		}

		if err == nil {
			for _, path := range strings.Split(build.Default.GOPATH, string(filepath.ListSeparator)) {
				utils.Logger.Infof("Checking import path %s with %s", currentPath, path)
				if strings.HasPrefix(currentPath, path) && len(currentPath) > len(path)+1 {
					importPath = currentPath[len(path)+1:]
					// Remove the source from the path if it is there
					if len(importPath) > 4 && (strings.ToLower(importPath[0:4]) == "src/" || strings.ToLower(importPath[0:4]) == "src\\") {
						importPath = importPath[4:]
					} else if importPath == "src" {
						if c.Index != VERSION {
							return fmt.Errorf("Invlaid import path, working dir is in GOPATH root")
						}
						importPath = ""
					}
					utils.Logger.Info("Updated import path", "path", importPath)
				}
			}
		}
	}

	c.ImportPath = importPath
	// We need the source root determined at this point to check the setversions
	c.initAppFolder()
	utils.Logger.Info("Returned import path", "path", importPath)
	if required && c.Index != NEW {
		if err := c.SetVersions(); err != nil {
			utils.Logger.Panic("Failed to fetch revel versions", "error", err)
		}
		if err := c.FrameworkVersion.CompatibleFramework(c); err != nil {
			utils.Logger.Fatal("Compatibility Error", "message", err,
				"Revel framework version", c.FrameworkVersion.String(), "Revel tool version", c.CommandVersion.String())
		}
		utils.Logger.Info("Revel versions", "revel-tool", c.CommandVersion.String(), "Revel Framework", c.FrameworkVersion.String())
	}
	if !required {
		return nil
	}
	if len(importPath) == 0 {
		return fmt.Errorf("Unable to determine import path from : %s", importPath)
	}
	return nil
}

func (c *CommandConfig) initAppFolder() (err error) {
	utils.Logger.Info("initAppFolder", "vendored", c.Vendored, "build-gopath", build.Default.GOPATH, "gopath-env", os.Getenv("GOPATH"))

	// check for go executable
	c.GoCmd, err = exec.LookPath("go")
	if err != nil {
		utils.Logger.Fatal("Go executable not found in PATH.")
	}

	wd, err := os.Getwd()

	modpath := GetModPath()
	if modpath != "" {
		bs, err := ioutil.ReadFile(filepath.Join(modpath, "go.mod"))
		if err != nil {
			utils.Logger.Fatal("Failed to read mod file:", "error", err)
		}

		pkgPath := modfile.ModulePath(bs)
		if c.ImportPath == pkgPath {
			c.AppPath = modpath
			utils.Logger.Info("Set application path and package based on go mod", "path", c.AppPath)
			return nil
		}

		if !strings.HasPrefix(c.ImportPath, pkgPath+"/") {
			utils.Logger.Fatal("Please run the command in the app path.")
			return nil
		}

		relatePath := strings.TrimPrefix(c.ImportPath, pkgPath+"/")
		c.AppPath = filepath.Join(modpath, filepath.FromSlash(relatePath))
		utils.Logger.Info("Set application path and package based on go mod", "path", c.AppPath)
		return nil
	}

	goenvpath := os.Getenv("GOPATH")
	goPathList := filepath.SplitList(goenvpath)
	bestpath := ""
	for _, path := range goPathList {
		if c.Index == NEW {
			// If the GOPATH is part of the working dir this is the most likely target
			if strings.HasPrefix(wd, path) {
				bestpath = path
			}
		} else if utils.Exists(filepath.Join(path, "src", c.ImportPath)) {
			c.AppPath = filepath.Join(bestpath, "src", c.ImportPath)
			utils.Logger.Info("Set application path and package based on go mod", "path", c.AppPath)
			return nil
		}
	}
	if bestpath != "" {
		c.AppPath = bestpath
		utils.Logger.Info("Set application path and package based on go mod", "path", c.AppPath)
		return nil
	}

	if filepath.IsAbs(c.ImportPath) {
		c.AppPath = c.ImportPath
	} else if c.Index == NEW {
		// This is new and not vendored, so the app path is the appFolder
		c.AppPath = filepath.Join(wd, c.ImportPath)
	} else {
		utils.Logger.Fatal("Please set gopaths or .")
		return nil
	}
	utils.Logger.Info("Set application path", "path", c.AppPath, "vendored", c.Vendored, "importpath", c.ImportPath)
	return nil
}

// Used to initialize the package resolver
func (c *CommandConfig) InitPackageResolver() {
	c.initGoPaths()
	utils.Logger.Info("InitPackageResolver", "useVendor", c.Vendored, "path", c.AppPath)

	// This should get called when needed
	c.PackageResolver = func(pkgName string) error {
		utils.Logger.Info("Request for package ", "package", pkgName, "use vendor", c.Vendored)
		var getCmd *exec.Cmd
		print("Downloading related packages ...")
		if c.Vendored {
			getCmd = exec.Command(c.GoCmd, "mod", "tidy")
		} else {
			utils.Logger.Info("No vendor folder detected, not using dependency manager to import package", "package", pkgName)
			getCmd = exec.Command(c.GoCmd, "get", "-u", pkgName)
		}

		utils.CmdInit(getCmd, !c.Vendored, c.AppPath)
		utils.Logger.Info("Go get command ", "exec", getCmd.Path, "dir", getCmd.Dir, "args", getCmd.Args, "env", getCmd.Env, "package", pkgName)
		output, err := getCmd.CombinedOutput()
		if err != nil {
			utils.Logger.Error("Failed to import package", "error", err, "gopath", build.Default.GOPATH, "GO-ROOT", build.Default.GOROOT, "output", string(output))
		}
		println(" completed.")

		return nil
	}
}

// lookup and set Go related variables
func (c *CommandConfig) initGoPaths() {
	utils.Logger.Info("InitGoPaths", "vendored", c.Vendored)
	// check for go executable
	var err error
	c.GoCmd, err = exec.LookPath("go")
	if err != nil {
		utils.Logger.Fatal("Go executable not found in PATH.")
	}

	if c.Vendored {
		return
	}

	// // lookup go path
	// c.GoPath = build.Default.GOPATH
	// if c.GoPath == "" {
	// 	utils.Logger.Fatal("Abort: GOPATH environment variable is not set. " +
	// 		"Please refer to http://golang.org/doc/code.html to configure your Go environment.")
	// }
	return
	//todo determine if the rest needs to happen

	// revel/revel#1004 choose go path relative to current working directory

	// What we want to do is to add the import to the end of the
	// gopath, and discover which import exists - If none exist this is an error except in the case
	// where we are dealing with new which is a special case where we will attempt to target the working directory first
	/*
		// If source root is empty and this isn't a version then skip it
		if len(c.SrcRoot) == 0 {
			if c.Index == NEW {
				c.SrcRoot = c.New.ImportPath
			} else {
				if c.Index != VERSION {
					utils.Logger.Fatal("Abort: could not create a Revel application outside of GOPATH.")
				}
				return
			}
		}

		// set go src path
		c.SrcRoot = filepath.Join(c.SrcRoot, "src")

		c.AppPath = filepath.Join(c.SrcRoot, filepath.FromSlash(c.ImportPath))
		utils.Logger.Info("Set application path", "path", c.AppPath)

	*/

}

// Sets the versions on the command config
func (c *CommandConfig) SetVersions() (err error) {
	c.CommandVersion, _ = ParseVersion(cmd.Version)
	pathMap, err := utils.FindSrcPaths(c.AppPath, []string{RevelImportPath}, c.PackageResolver)
	if err == nil {
		utils.Logger.Info("Fullpath to revel", "dir", pathMap[RevelImportPath])
		fset := token.NewFileSet() // positions are relative to fset

		versionData, err := ioutil.ReadFile(filepath.Join(pathMap[RevelImportPath], "version.go"))
		if err != nil {
			utils.Logger.Error("Failed to find Revel version:", "error", err, "path", pathMap[RevelImportPath])
		}

		// Parse src but stop after processing the imports.
		f, err := parser.ParseFile(fset, "", versionData, parser.ParseComments)
		if err != nil {
			return utils.NewBuildError("Failed to parse Revel version error:", "error", err)
		}

		// Print the imports from the file's AST.
		for _, s := range f.Decls {
			genDecl, ok := s.(*ast.GenDecl)
			if !ok {
				continue
			}
			if genDecl.Tok != token.CONST {
				continue
			}
			for _, a := range genDecl.Specs {
				spec := a.(*ast.ValueSpec)
				r := spec.Values[0].(*ast.BasicLit)
				if spec.Names[0].Name == "Version" {
					c.FrameworkVersion, err = ParseVersion(strings.Replace(r.Value, `"`, ``, -1))
					if err != nil {
						utils.Logger.Errorf("Failed to parse version")
					} else {
						utils.Logger.Info("Parsed revel version", "version", c.FrameworkVersion.String())
					}
				}
			}
		}
	}
	return
}

func GetModPath() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		gomod := filepath.Join(wd, "go.mod")
		if _, err := os.Stat(gomod); err == nil {
			return wd
		}
		parent := filepath.Dir(wd)
		if len(parent) >= len(wd) {
			return ""
		}
		wd = parent
	}
}
