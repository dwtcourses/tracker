package main

import (
	"fmt"
	"net"
	"os"

	"github.com/deps-cloud/tracker/api"
	"github.com/deps-cloud/tracker/api/v1alpha/store"
	"github.com/deps-cloud/tracker/pkg/service"
	"github.com/deps-cloud/tracker/pkg/services"
	"github.com/deps-cloud/tracker/pkg/services/graphstore"

	_ "github.com/go-sql-driver/mysql"

	"github.com/jmoiron/sqlx"

	_ "github.com/mattn/go-sqlite3"

	"github.com/sirupsen/logrus"

	"github.com/spf13/cobra"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

func panicIff(err error) {
	if err != nil {
		logrus.Error(err.Error())
		os.Exit(1)
	}
}

func registerV1Alpha(rwdb, rodb *sqlx.DB, statements *graphstore.Statements, server *grpc.Server) {
	graphStore, err := graphstore.NewSQLGraphStore(rwdb, rodb, statements)
	panicIff(err)

	graphStoreClient := store.NewInProcessGraphStoreClient(graphStore)

	// poc
	dts, _ := service.NewDependencyTrackingService(graphStoreClient)
	api.RegisterDependencyTrackerServer(server, dts)

	// v1alpha
	services.RegisterDependencyService(server, graphStoreClient)
	services.RegisterModuleService(server, graphStoreClient)
	services.RegisterSourceService(server, graphStoreClient)
	services.RegisterTopologyService(server, graphStoreClient)
}

func main() {
	configPath := "${HOME}/.dts/config.yaml"
	port := 8090
	storageDriver := "sqlite3"
	storageAddress := "file::memory:?cache=shared"
	storageReadOnlyAddress := ""
	storageStatementsFile := ""
	tlsKey := ""
	tlsCert := ""

	cmd := &cobra.Command{
		Use:   "tracker",
		Short: "tracker runs the dependency tracking service.",
		Run: func(cmd *cobra.Command, args []string) {
			var rwdb *sqlx.DB
			var err error

			if len(storageAddress) > 0 {
				rwdb, err = sqlx.Open(storageDriver, storageAddress)
				panicIff(err)
			}

			rodb := rwdb
			if len(storageReadOnlyAddress) > 0 {
				rodb, err = sqlx.Open(storageDriver, storageReadOnlyAddress)
				panicIff(err)
			}

			if rodb == nil && rwdb == nil {
				panicIff(fmt.Errorf("either --storage-address or --storage-readonly-address must be provided"))
			}

			statements := graphstore.DefaultStatements()
			if len(storageStatementsFile) > 0 {
				statements, err = graphstore.LoadStatementsFile(storageStatementsFile)
				panicIff(err)
			}

			options := make([]grpc.ServerOption, 0)
			if len(tlsCert) > 0 && len(tlsKey) > 0 {
				logrus.Info("[main] configuring tls")

				creds, err := credentials.NewServerTLSFromFile(tlsCert, tlsKey)
				panicIff(err)

				options = append(options, grpc.Creds(creds))
			}

			server := grpc.NewServer(options...)
			healthpb.RegisterHealthServer(server, health.NewServer())
			registerV1Alpha(rwdb, rodb, statements, server)

			// setup server
			address := fmt.Sprintf(":%d", port)

			listener, err := net.Listen("tcp", address)
			panicIff(err)

			logrus.Infof("[main] starting gRPC on %s", address)
			err = server.Serve(listener)
			panicIff(err)
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&configPath, "config", configPath, "(optional) the path to the config file")
	flags.IntVar(&port, "port", port, "(optional) the port to run on")
	flags.StringVar(&storageDriver, "storage-driver", storageDriver, "(optional) the driver used to configure the storage tier")
	flags.StringVar(&storageAddress, "storage-address", storageAddress, "(optional) the address of the storage tier")
	flags.StringVar(&storageReadOnlyAddress, "storage-readonly-address", storageReadOnlyAddress, "(optional) the readonly address of the storage tier")
	flags.StringVar(&storageStatementsFile, "storage-statements-file", storageStatementsFile, "(optional) path to a yaml file containing the definition of each SQL statement")
	flags.StringVar(&tlsKey, "tls-key", tlsKey, "(optional) path to the file containing the TLS private key")
	flags.StringVar(&tlsCert, "tls-cert", tlsCert, "(optional) path to the file containing the TLS certificate")

	err := cmd.Execute()
	panicIff(err)
}
