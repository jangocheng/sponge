package generate

import (
	"errors"
	"fmt"
	"math/rand"
	"strings"

	"github.com/zhufuyi/sponge/pkg/gofile"
	"github.com/zhufuyi/sponge/pkg/replacer"
	"github.com/zhufuyi/sponge/pkg/sql2code"
	"github.com/zhufuyi/sponge/pkg/sql2code/parser"

	"github.com/huandu/xstrings"
	"github.com/spf13/cobra"
)

// RPCCommand generate grpc service code
func RPCCommand() *cobra.Command {
	var (
		moduleName  string // module name for go.mod
		serverName  string // server name
		projectName string // project name for deployment name
		repoAddr    string // image repo address
		outPath     string // output directory
		dbTables    string // table names
		sqlArgs     = sql2code.Args{
			Package:  "model",
			JSONTag:  true,
			GormType: true,
		}
	)

	//nolint
	cmd := &cobra.Command{
		Use:   "rpc",
		Short: "Generate grpc service code based on sql",
		Long: `generate grpc service code based on sql.

Examples:
  # generate grpc service code and embed gorm.model struct.
  sponge micro rpc --module-name=yourModuleName --server-name=yourServerName --project-name=yourProjectName --db-driver=mysql --db-dsn=root:123456@(192.168.3.37:3306)/test --db-table=user

  # generate grpc service code, structure fields correspond to the column names of the table.
  sponge micro rpc --module-name=yourModuleName --server-name=yourServerName --project-name=yourProjectName --db-driver=mysql --db-dsn=root:123456@(192.168.3.37:3306)/test --db-table=user --embed=false

  # generate grpc service code with multiple table names.
  sponge micro rpc --module-name=yourModuleName --server-name=yourServerName --project-name=yourProjectName --db-driver=mysql --db-dsn=root:123456@(192.168.3.37:3306)/test --db-table=t1,t2

  # generate grpc service code and specify the output directory, Note: code generation will be canceled when the latest generated file already exists.
  sponge micro rpc --module-name=yourModuleName --server-name=yourServerName --project-name=yourProjectName --db-driver=mysql --db-dsn=root:123456@(192.168.3.37:3306)/test --db-table=user --out=./yourServerDir

  # generate grpc service code and specify the docker image repository address.
  sponge micro rpc --module-name=yourModuleName --server-name=yourServerName --project-name=yourProjectName --repo-addr=192.168.3.37:9443/user-name --db-driver=mysql --db-dsn=root:123456@(192.168.3.37:3306)/test --db-table=user
`,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			var firstTable string
			var servicesTableNames []string
			tableNames := strings.Split(dbTables, ",")
			if len(tableNames) == 1 {
				firstTable = tableNames[0]
			} else if len(tableNames) > 1 {
				firstTable = tableNames[0]
				servicesTableNames = tableNames[1:]
			}

			projectName, serverName = convertProjectAndServerName(projectName, serverName)
			if sqlArgs.DBDriver == DBDriverMongodb {
				sqlArgs.IsEmbed = false
			}

			sqlArgs.DBTable = firstTable
			codes, err := sql2code.Generate(&sqlArgs)
			if err != nil {
				return err
			}
			g := &rpcGenerator{
				moduleName:  moduleName,
				serverName:  serverName,
				projectName: projectName,
				repoAddr:    repoAddr,
				dbDSN:       sqlArgs.DBDsn,
				dbDriver:    sqlArgs.DBDriver,
				isEmbed:     sqlArgs.IsEmbed,
				codes:       codes,
				outPath:     outPath,
			}
			outPath, err = g.generateCode()
			if err != nil {
				return err
			}

			for _, serviceTableName := range servicesTableNames {
				if serviceTableName == "" {
					continue
				}

				sqlArgs.DBTable = serviceTableName
				codes, err := sql2code.Generate(&sqlArgs)
				if err != nil {
					return err
				}

				sg := &serviceGenerator{
					moduleName: moduleName,
					serverName: serverName,
					dbDriver:   sqlArgs.DBDriver,
					isEmbed:    sqlArgs.IsEmbed,
					codes:      codes,
					outPath:    outPath,
				}
				outPath, err = sg.generateCode()
				if err != nil {
					return err
				}
			}

			fmt.Printf(`
using help:
  1. open a terminal and execute the command to generate code:  make proto
  2. compile and run service:   make run
  3. open the file internal/service/xxx_client_test.go using Goland or VS Code, and test CRUD api interface.

`)
			fmt.Printf("generate %s's grpc service code successfully, out = %s\n", serverName, outPath)

			_ = generateConfigmap(serverName, outPath)
			return nil
		},
	}

	cmd.Flags().StringVarP(&moduleName, "module-name", "m", "", "module-name is the name of the module in the go.mod file")
	_ = cmd.MarkFlagRequired("module-name")
	cmd.Flags().StringVarP(&serverName, "server-name", "s", "", "server name")
	_ = cmd.MarkFlagRequired("server-name")
	cmd.Flags().StringVarP(&projectName, "project-name", "p", "", "project name")
	_ = cmd.MarkFlagRequired("project-name")
	cmd.Flags().StringVarP(&sqlArgs.DBDriver, "db-driver", "k", "mysql", "database driver, support mysql, mongodb, postgresql, tidb, sqlite")
	cmd.Flags().StringVarP(&sqlArgs.DBDsn, "db-dsn", "d", "", "database content address, e.g. user:password@(host:port)/database. Note: if db-driver=sqlite, db-dsn must be a local sqlite db file, e.g. --db-dsn=/tmp/sponge_sqlite.db") //nolint
	_ = cmd.MarkFlagRequired("db-dsn")
	cmd.Flags().StringVarP(&dbTables, "db-table", "t", "", "table name, multiple names separated by commas")
	_ = cmd.MarkFlagRequired("db-table")
	cmd.Flags().BoolVarP(&sqlArgs.IsEmbed, "embed", "e", true, "whether to embed gorm.model struct")
	cmd.Flags().IntVarP(&sqlArgs.JSONNamedType, "json-name-type", "j", 1, "json tags name type, 0:snake case, 1:camel case")
	cmd.Flags().StringVarP(&repoAddr, "repo-addr", "r", "", "docker image repository address, excluding http and repository names")
	cmd.Flags().StringVarP(&outPath, "out", "o", "", "output directory, default is ./serverName_rpc_<time>")

	return cmd
}

type rpcGenerator struct {
	moduleName  string
	serverName  string
	projectName string
	repoAddr    string
	dbDSN       string
	dbDriver    string
	isEmbed     bool
	codes       map[string]string
	outPath     string
}

func (g *rpcGenerator) generateCode() (string, error) {
	subTplName := "rpc"
	r := Replacers[TplNameSponge]
	if r == nil {
		return "", errors.New("replacer is nil")
	}

	// setting up template information
	subDirs := []string{ // specify the subdirectory for processing
		"sponge/api", "cmd/serverNameExample_grpcExample", "sponge/configs", "sponge/deployments",
		"sponge/scripts", "sponge/internal", "sponge/third_party",
	}
	subFiles := []string{ // specify the sub-documents to be processed
		"sponge/.gitignore", "sponge/.golangci.yml", "sponge/go.mod", "sponge/go.sum",
		"sponge/Jenkinsfile", "sponge/Makefile", "sponge/README.md",
	}
	ignoreDirs := []string{ // specify the directory in the subdirectory where processing is ignored
		"internal/handler", "internal/rpcclient", "internal/routers", "internal/types", "cmd/sponge",
	}
	var ignoreFiles []string
	switch strings.ToLower(g.dbDriver) {
	case DBDriverMysql, DBDriverPostgresql, DBDriverTidb, DBDriverSqlite:
		ignoreFiles = []string{ // specify the files in the subdirectory to be ignored for processing
			"userExample_http.go", "systemCode_http.go", // internal/ecode
			"http.go", "http_option.go", "http_test.go", // internal/server
			"scripts/swag-docs.sh",                // sponge/scripts
			"types.pb.validate.go", "types.pb.go", // api/types
			"userExample.pb.go", "userExample.pb.validate.go", "userExample_grpc.pb.go", "userExample_router.pb.go", // api/serverNameExample/v1
			"init_test.go", "init.go.mgo", // model
			"doc.go", "cacheNameExample.go", "cacheNameExample_test.go", "cache/userExample.go.mgo", // internal/cache
			"dao/userExample.go.mgo",                                                                                                                                   // internal/dao
			"userExample_logic.go", "userExample_logic_test.go", "service/userExample_test.go", "service/userExample.go.mgo", "service/userExample_client_test.go.mgo", // internal/service
		}
	case DBDriverMongodb:
		ignoreFiles = []string{ // specify the files in the subdirectory to be ignored for processing
			"userExample_http.go", "systemCode_http.go", // internal/ecode
			"http.go", "http_option.go", "http_test.go", // internal/server
			"scripts/swag-docs.sh",                // sponge/scripts
			"types.pb.validate.go", "types.pb.go", // api/types
			"userExample.pb.go", "userExample.pb.validate.go", "userExample_grpc.pb.go", "userExample_router.pb.go", // api/serverNameExample/v1
			"init_test.go", "init.go", // model
			"doc.go", "cacheNameExample.go", "cacheNameExample_test.go", "cache/userExample.go", "cache/userExample_test.go", // internal/cache
			"dao/userExample_test.go", "dao/userExample.go", // internal/dao
			"userExample_logic.go", "userExample_logic_test.go", "service/userExample_test.go", "service/userExample.go", "service/userExample_client_test.go", // internal/service
		}
	default:
		return "", errors.New("unsupported db driver: " + g.dbDriver)
	}

	r.SetSubDirsAndFiles(subDirs, subFiles...)
	r.SetIgnoreSubDirs(ignoreDirs...)
	r.SetIgnoreSubFiles(ignoreFiles...)
	fields := g.addFields(r)
	r.SetReplacementFields(fields)
	_ = r.SetOutputDir(g.outPath, g.serverName+"_"+subTplName)
	if err := r.SaveFiles(); err != nil {
		return "", err
	}
	_ = saveGenInfo(g.moduleName, g.serverName, r.GetOutputDir())

	return r.GetOutputDir(), nil
}

func (g *rpcGenerator) addFields(r replacer.Replacer) []replacer.Field {
	var fields []replacer.Field

	repoHost, _ := parseImageRepoAddr(g.repoAddr)

	fields = append(fields, deleteFieldsMark(r, modelFile, startMark, endMark)...)
	fields = append(fields, deleteFieldsMark(r, modelInitDBFile, startMark, endMark)...)
	fields = append(fields, deleteFieldsMark(r, daoFile, startMark, endMark)...)
	fields = append(fields, deleteFieldsMark(r, daoMgoFile, startMark, endMark)...)
	fields = append(fields, deleteFieldsMark(r, daoTestFile, startMark, endMark)...)
	fields = append(fields, deleteFieldsMark(r, protoFile, startMark, endMark)...)
	fields = append(fields, deleteFieldsMark(r, serviceLogicFile, startMark, endMark)...)
	fields = append(fields, deleteFieldsMark(r, serviceClientFile, startMark, endMark)...)
	fields = append(fields, deleteFieldsMark(r, serviceClientMgoFile, startMark, endMark)...)
	fields = append(fields, deleteFieldsMark(r, serviceTestFile, startMark, endMark)...)
	fields = append(fields, deleteFieldsMark(r, dockerFile, wellStartMark, wellEndMark)...)
	fields = append(fields, deleteFieldsMark(r, dockerFileBuild, wellStartMark, wellEndMark)...)
	fields = append(fields, deleteFieldsMark(r, dockerComposeFile, wellStartMark, wellEndMark)...)
	fields = append(fields, deleteFieldsMark(r, k8sDeploymentFile, wellStartMark, wellEndMark)...)
	fields = append(fields, deleteFieldsMark(r, k8sServiceFile, wellStartMark, wellEndMark)...)
	fields = append(fields, deleteFieldsMark(r, imageBuildFile, wellStartMark, wellEndMark)...)
	fields = append(fields, deleteFieldsMark(r, imageBuildLocalFile, wellStartMark, wellEndMark)...)
	fields = append(fields, deleteAllFieldsMark(r, makeFile, wellStartMark, wellEndMark)...)
	fields = append(fields, deleteFieldsMark(r, gitIgnoreFile, wellStartMark, wellEndMark)...)
	fields = append(fields, deleteAllFieldsMark(r, protoShellFile, wellStartMark, wellEndMark)...)
	fields = append(fields, deleteAllFieldsMark(r, appConfigFile, wellStartMark, wellEndMark)...)
	//fields = append(fields, deleteFieldsMark(r, deploymentConfigFile, wellStartMark, wellEndMark)...)
	fields = append(fields, replaceFileContentMark(r, readmeFile, "## "+g.serverName)...)
	fields = append(fields, []replacer.Field{
		{ // replace the configuration of the *.yml file
			Old: appConfigFileMark,
			New: rpcServerConfigCode,
		},
		{ // replace the configuration of the *.yml file
			Old: appConfigFileMark2,
			New: getDBConfigCode(g.dbDriver),
		},
		{ // replace the contents of the model/userExample.go file
			Old: modelFileMark,
			New: g.codes[parser.CodeTypeModel],
		},
		{ // replace the contents of the model/init.go file
			Old: modelInitDBFileMark,
			New: getInitDBCode(g.dbDriver),
		},
		{ // replace the contents of the dao/userExample.go file
			Old: daoFileMark,
			New: g.codes[parser.CodeTypeDAO],
		},
		{ // replace the contents of the handler/userExample_logic.go file
			Old: embedTimeMark,
			New: getEmbedTimeCode(g.isEmbed),
		},
		{ // replace the contents of the v1/userExample.proto file
			Old: protoFileMark,
			New: g.codes[parser.CodeTypeProto],
		},
		{ // replace the contents of the proto.sh file
			Old: protoShellFileGRPCMark,
			New: protoShellGRPCMark,
		},
		{ // replace the contents of the scripts/proto.sh file
			Old: protoShellFileMark,
			New: protoShellServiceTmplCode,
		},
		{ // replace the contents of the service/userExample_client_test.go file
			Old: serviceFileMark,
			New: adjustmentOfIDType(g.codes[parser.CodeTypeService], g.dbDriver),
		},
		{ // replace the contents of the Dockerfile file
			Old: dockerFileMark,
			New: dockerFileGrpcCode,
		},
		{ // replace the contents of the Dockerfile_build file
			Old: dockerFileBuildMark,
			New: dockerFileBuildGrpcCode,
		},
		{ // replace the contents of the image-build.sh file
			Old: imageBuildFileMark,
			New: imageBuildFileGrpcCode,
		},
		{ // replace the contents of the image-build-local.sh file
			Old: imageBuildLocalFileMark,
			New: imageBuildLocalFileGrpcCode,
		},
		{ // replace the contents of the docker-compose.yml file
			Old: dockerComposeFileMark,
			New: dockerComposeFileGrpcCode,
		},
		//{ // replace the contents of the *-configmap.yml file
		//	Old: deploymentConfigFileMark,
		//	New: getDBConfigCode(g.dbDriver, true),
		//},
		{ // replace the contents of the *-deployment.yml file
			Old: k8sDeploymentFileMark,
			New: k8sDeploymentFileGrpcCode,
		},
		{ // replace the contents of the *-svc.yml file
			Old: k8sServiceFileMark,
			New: k8sServiceFileGrpcCode,
		},
		{ // replace github.com/zhufuyi/sponge/templates/sponge
			Old: selfPackageName + "/" + r.GetSourcePath(),
			New: g.moduleName,
		},
		// replace directory name
		{
			Old: strings.Join([]string{"api", "userExample", "v1"}, gofile.GetPathDelimiter()),
			New: strings.Join([]string{"api", g.serverName, "v1"}, gofile.GetPathDelimiter()),
		},
		{
			Old: "github.com/zhufuyi/sponge",
			New: g.moduleName,
		},
		{
			Old: g.moduleName + "/pkg",
			New: "github.com/zhufuyi/sponge/pkg",
		},
		{ // replace the sponge version of the go.mod file
			Old: spongeTemplateVersionMark,
			New: getLocalSpongeTemplateVersion(),
		},
		{
			Old: "api/userExample/v1",
			New: fmt.Sprintf("api/%s/v1", g.serverName),
		},
		{
			Old: "api.userExample.v1",
			New: fmt.Sprintf("api.%s.v1", g.serverName), // protobuf package no "-" signs allowed
		},
		{
			Old: "sponge api docs",
			New: g.serverName + " api docs",
		},
		{
			Old: "_userExampleNO       = 2",
			New: fmt.Sprintf("_userExampleNO       = %d", rand.Intn(100)),
		},
		{
			Old: "serverNameExample",
			New: g.serverName,
		},
		// docker image and k8s deployment script replacement
		{
			Old: "server-name-example",
			New: xstrings.ToKebabCase(g.serverName), // snake_case to kebab_case
		},
		// docker image and k8s deployment script replacement
		{
			Old: "project-name-example",
			New: g.projectName,
		},
		{
			Old: "projectNameExample",
			New: g.projectName,
		},
		{
			Old: "repo-addr-example",
			New: g.repoAddr,
		},
		{
			Old: "image-repo-host",
			New: repoHost,
		},
		{
			Old: "_grpcExample",
			New: "",
		},
		{
			Old: "_mixExample",
			New: "",
		},
		{
			Old: "root:123456@(192.168.3.37:3306)/account",
			New: g.dbDSN,
		},
		{
			Old: "root:123456@192.168.3.37:5432/account",
			New: g.dbDSN,
		},
		{
			Old: "test/sql/sqlite/sponge.db",
			New: sqliteDSNAdaptation(g.dbDriver, g.dbDSN),
		},
		{
			Old: "init.go.mgo",
			New: "init.go",
		},
		{
			Old: "userExample_client_test.go.mgo",
			New: "userExample_client_test.go",
		},
		{
			Old: "userExample.go.mgo",
			New: "userExample.go",
		},
		{
			Old:             "UserExample",
			New:             g.codes[parser.TableName],
			IsCaseSensitive: true,
		},
	}...)

	return fields
}
