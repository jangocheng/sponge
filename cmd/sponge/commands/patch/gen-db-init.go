package patch

import (
	"errors"
	"fmt"
	"strings"

	"github.com/zhufuyi/sponge/cmd/sponge/commands/generate"
	"github.com/zhufuyi/sponge/pkg/gofile"
	"github.com/zhufuyi/sponge/pkg/replacer"

	"github.com/spf13/cobra"
)

// GenerateDBInitCommand generate database initialization code
func GenerateDBInitCommand() *cobra.Command {
	var (
		moduleName string // go.mod module name
		dbDriver   string // database driver e.g. mysql, mongodb, postgresql, tidb, sqlite
		outPath    string // output directory
		targetFile = "internal/model/init.go"
	)

	cmd := &cobra.Command{
		Use:   "gen-db-init",
		Short: "Generate database initialization code",
		Long: `generate database initialization code.

Examples:
  # generate mysql initialization code.
  sponge patch gen-db-init --module-name=yourModuleName --db-driver=mysql

  # generate mysql initialization code, and specify the server directory, Note: code generation will be canceled when the latest generated file already exists.
  sponge patch gen-db-init --db-driver=mysql --out=./yourServerDir
`,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			mdName, _ := getNamesFromOutDir(outPath)
			if mdName != "" {
				moduleName = mdName
			} else if moduleName == "" {
				return fmt.Errorf(`required flag(s) "module-name" not set, use "sponge patch gen-db-init -h" for help`)
			}

			var isEmpty bool
			if outPath == "" {
				isEmpty = true
			} else {
				isEmpty = false
				if gofile.IsExists(targetFile) {
					fmt.Printf("'%s' already exists, no need to generate it.\n", targetFile)
					return nil
				}
			}

			g := &dbInitGenerator{
				moduleName: moduleName,
				dbDriver:   dbDriver,
				outPath:    outPath,
			}
			var err error
			outPath, err = g.generateCode()
			if err != nil {
				return err
			}

			if isEmpty {
				fmt.Printf(`
using help:
  move the folder "internal" to your project code folder.

`)
			}
			if gofile.IsWindows() {
				targetFile = "\\" + strings.ReplaceAll(targetFile, "/", "\\")
			} else {
				targetFile = "/" + targetFile
			}
			fmt.Printf("generate \"%s-init\" codes successfully, out = %s\n", dbDriver, outPath+targetFile)
			return nil
		},
	}

	cmd.Flags().StringVarP(&dbDriver, "db-driver", "k", "mysql", "database driver, support mysql, mongodb, postgresql, tidb, sqlite")
	cmd.Flags().StringVarP(&moduleName, "module-name", "m", "", "module-name is the name of the module in the 'go.mod' file")
	cmd.Flags().StringVarP(&outPath, "out", "o", "", "output directory, default is ./mysql-init_<time>, "+
		"if you specify the directory where the web or microservice generated by sponge, the module-name flag can be ignored")

	return cmd
}

type dbInitGenerator struct {
	moduleName string
	dbDriver   string
	outPath    string
}

func (g *dbInitGenerator) generateCode() (string, error) {
	subTplName := "init-" + g.dbDriver
	r := generate.Replacers[generate.TplNameSponge]
	if r == nil {
		return "", errors.New("replacer is nil")
	}

	// setting up template information
	subDirs := []string{"internal/model"} // only the specified subdirectory is processed, if empty or no subdirectory is specified, it means all files
	ignoreDirs := []string{}              // specify the directory in the subdirectory where processing is ignored
	var ignoreFiles []string
	switch strings.ToLower(g.dbDriver) {
	case generate.DBDriverMysql, generate.DBDriverPostgresql, generate.DBDriverTidb, generate.DBDriverSqlite:
		ignoreFiles = []string{ // specify the files in the subdirectory to be ignored for processing
			"userExample.go", "init_test.go", "init.go.mgo",
		}
	case generate.DBDriverMongodb:
		ignoreFiles = []string{ // specify the files in the subdirectory to be ignored for processing
			"userExample.go", "init_test.go", "init.go",
		}
	default:
		return "", fmt.Errorf("unsupported database driver: %s", g.dbDriver)
	}

	r.SetSubDirsAndFiles(subDirs)
	r.SetIgnoreSubDirs(ignoreDirs...)
	r.SetIgnoreSubFiles(ignoreFiles...)
	fields := g.addFields(r)
	r.SetReplacementFields(fields)
	_ = r.SetOutputDir(g.outPath, subTplName)
	if err := r.SaveFiles(); err != nil {
		return "", err
	}

	return r.GetOutputDir(), nil
}

func (g *dbInitGenerator) addFields(r replacer.Replacer) []replacer.Field {
	var fields []replacer.Field

	fields = append(fields, generate.DeleteCodeMark(r, generate.ModelInitDBFile, generate.StartMark, generate.EndMark)...)
	fields = append(fields, []replacer.Field{
		{
			Old:             "github.com/zhufuyi/sponge/internal",
			New:             g.moduleName + "/internal",
			IsCaseSensitive: false,
		},
		{
			Old:             "github.com/zhufuyi/sponge/configs",
			New:             g.moduleName + "/configs",
			IsCaseSensitive: false,
		},
		{ // rename init.go.mgo --> init.go
			Old: "init.go.mgo",
			New: "init.go",
		},
		{ // replace the contents of the model/init.go file
			Old: generate.ModelInitDBFileMark,
			New: generate.GetInitDataBaseCode(g.dbDriver),
		},
	}...)

	return fields
}
