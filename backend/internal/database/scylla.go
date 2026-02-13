package database

import (
	"fmt"

	"github.com/savanp08/converse/internal/config"

	"github.com/gocql/gocql"
)

type ScyllaStore struct {
	Session *gocql.Session
}

func NewScyllaStore(cfg config.Config) (*ScyllaStore, error) {
	cluster := gocql.NewCluster(cfg.ScyllaHosts...)
	cluster.Keyspace = cfg.ScyllaKeyspace
	cluster.Consistency = gocql.Quorum

	session, err := cluster.CreateSession()
	if err != nil {
		return nil, fmt.Errorf("create scylla session: %w", err)
	}

	return &ScyllaStore{Session: session}, nil
}

func (s *ScyllaStore) Close() {
	s.Session.Close()
}
