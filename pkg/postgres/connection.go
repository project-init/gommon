package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/rds/auth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	gerror "github.com/project-init/gommon/pkg/errors"
)

type ConnectionConfig struct {
	Host     string
	Port     string
	User     string
	Database string
	Password string

	MaxConnections  *int
	MinConnections  *int
	MaxConnLifetime *string
	MaxConnIdleTime *string
}
type IAMConfig struct {
	Credentials aws.CredentialsProvider
	Region      string
}

// NewConnection Creates a connection pool to the database defined in the ConnectionConfig. If an IAMConfig is given,
// then it is assumed to be using rds connect and the connection lifetime values will be overwritten.
func NewConnection(ctx context.Context, connectionConfig *ConnectionConfig, iamConfig *IAMConfig) (*pgxpool.Pool, error) {
	connStr, err := connectionString(ctx, connectionConfig, iamConfig)
	if err != nil {
		return nil, err
	}

	pgxCfg, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return nil, fmt.Errorf("parse postgres conn string: %w", err)
	}

	if iamConfig != nil {
		pgxCfg.BeforeConnect = func(ctx context.Context, config *pgx.ConnConfig) error {
			authToken, err := BuildAuthToken(ctx, *connectionConfig, *iamConfig)
			if err != nil {
				return err
			}
			config.Password = authToken
			return nil
		}
	}

	pool, err := pgxpool.NewWithConfig(ctx, pgxCfg)
	if err != nil {
		return nil, fmt.Errorf("%w: could not connect to postgres", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("%w: could not ping postgres", err)
	}

	return pool, nil
}

func connectionString(ctx context.Context, connectionConfig *ConnectionConfig, iamConfig *IAMConfig) (string, error) {
	if connectionConfig == nil {
		return "", fmt.Errorf("%w: connection config is required", gerror.ErrPreconditionFailed)
	}

	maxConnections := 5
	minConnections := 1
	// Setting Max connection lifetime and idle time to 14m as the token we get from iam is good for only 15m
	maxConnLifetime := "14m"
	maxConnIdleTime := "14m"

	password := connectionConfig.Password
	sslMode := "disable"
	if iamConfig != nil {
		authToken, err := BuildAuthToken(ctx, *connectionConfig, *iamConfig)
		if err != nil {
			return "", err
		}
		password = authToken
		sslMode = "require"
	}

	if connectionConfig.MaxConnections != nil && *connectionConfig.MaxConnections > 0 {
		maxConnections = *connectionConfig.MaxConnections
	}
	if connectionConfig.MinConnections != nil && *connectionConfig.MinConnections > 0 {
		minConnections = *connectionConfig.MinConnections
	}
	// If iamConfig is given, then do not override the default for connection lifetime and idle time.
	if connectionConfig.MaxConnLifetime != nil && iamConfig == nil {
		maxConnLifetime = *connectionConfig.MaxConnLifetime
	}
	if connectionConfig.MaxConnIdleTime != nil && iamConfig == nil {
		maxConnIdleTime = *connectionConfig.MaxConnIdleTime
	}

	hostInfo := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s", connectionConfig.Host, connectionConfig.Port, connectionConfig.User, password, connectionConfig.Database, sslMode)
	connectionInfo := fmt.Sprintf("pool_max_conns=%d pool_min_conns=%d pool_max_conn_lifetime=%s pool_max_conn_idle_time=%s", maxConnections, minConnections, maxConnLifetime, maxConnIdleTime)
	return fmt.Sprintf("%s %s", hostInfo, connectionInfo), nil
}

func BuildAuthToken(ctx context.Context, connectionConfig ConnectionConfig, iamConfig IAMConfig) (string, error) {
	return auth.BuildAuthToken(ctx, fmt.Sprintf("%s:%s", connectionConfig.Host, connectionConfig.Port), iamConfig.Region, connectionConfig.User, iamConfig.Credentials)
}
