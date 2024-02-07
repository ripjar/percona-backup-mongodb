package snapshot

import (
	"io"

	"github.com/mongodb/mongo-tools/common/options"
	"github.com/mongodb/mongo-tools/mongorestore"
	"go.mongodb.org/mongo-driver/mongo/writeconcern"

	"github.com/percona/percona-backup-mongodb/internal/config"
	"github.com/percona/percona-backup-mongodb/internal/defs"
	"github.com/percona/percona-backup-mongodb/internal/errors"
)

const (
	preserveUUID = true

	batchSizeDefault           = 500
	numInsertionWorkersDefault = 10
)

var ExcludeFromRestore = []string{
	defs.DB + "." + defs.CmdStreamCollection,
	defs.DB + "." + defs.LogCollection,
	defs.DB + "." + defs.ConfigCollection,
	defs.DB + "." + defs.BcpCollection,
	defs.DB + "." + defs.RestoresCollection,
	defs.DB + "." + defs.LockCollection,
	defs.DB + "." + defs.LockOpCollection,
	defs.DB + "." + defs.PITRChunksCollection,
	defs.DB + "." + defs.AgentsStatusCollection,
	defs.DB + "." + defs.PBMOpLogCollection,
	"admin.system.version",
	"config.version",
	"config.mongos",
	"config.lockpings",
	"config.locks",
	"config.system.sessions",
	"config.cache.*",
	"config.shards",
	"config.transactions",
	"config.transaction_coordinators",
	"config.changelog",
	"config.actionlog",
	"config.image_collection",
	"config.system.indexBuilds",
	"config.system.sharding_ddl_coordinators",

	// deprecated PBM collections, keep it here not to bring back from old backups
	defs.DB + ".pbmBackups.old",
	defs.DB + ".pbmPITRChunks.old",
}

type restorer struct{ *mongorestore.MongoRestore }

func NewRestore(uri string, cfg *config.Config) (io.ReaderFrom, error) {
	topts := options.New("mongorestore",
		"0.0.1",
		"none",
		"",
		true,
		options.EnabledOptions{
			Auth:       true,
			Connection: true,
			Namespace:  true,
			URI:        true,
		})
	var err error
	topts.URI, err = options.NewURI(uri)
	if err != nil {
		return nil, errors.Wrap(err, "parse connection string")
	}

	err = topts.NormalizeOptionsAndURI()
	if err != nil {
		return nil, errors.Wrap(err, "parse opts")
	}

	topts.Direct = true
	topts.WriteConcern = writeconcern.Majority()

	batchSize := batchSizeDefault
	if cfg.Restore.BatchSize > 0 {
		batchSize = cfg.Restore.BatchSize
	}
	numInsertionWorkers := numInsertionWorkersDefault
	if cfg.Restore.NumInsertionWorkers > 0 {
		numInsertionWorkers = cfg.Restore.NumInsertionWorkers
	}

	mopts := mongorestore.Options{}
	mopts.ToolOptions = topts
	mopts.InputOptions = &mongorestore.InputOptions{
		Archive: "-",
	}
	mopts.OutputOptions = &mongorestore.OutputOptions{
		BulkBufferSize:           batchSize,
		BypassDocumentValidation: true,
		Drop:                     true,
		NumInsertionWorkers:      numInsertionWorkers,
		NumParallelCollections:   1,
		PreserveUUID:             preserveUUID,
		StopOnError:              true,
		WriteConcern:             "majority",
		NoIndexRestore:           true,
	}
	mopts.NSOptions = &mongorestore.NSOptions{
		NSExclude: ExcludeFromRestore,
	}

	mr, err := mongorestore.New(mopts)
	if err != nil {
		return nil, errors.Wrap(err, "create mongorestore obj")
	}
	mr.SkipUsersAndRoles = true

	return &restorer{mr}, nil
}

func (r *restorer) ReadFrom(from io.Reader) (int64, error) {
	defer r.Close()

	r.InputReader = from

	rdumpResult := r.Restore()
	if rdumpResult.Err != nil {
		return 0, errors.Wrapf(rdumpResult.Err, "restore mongo dump (successes: %d / fails: %d)",
			rdumpResult.Successes, rdumpResult.Failures)
	}

	return 0, nil
}
