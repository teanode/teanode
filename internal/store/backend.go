package store

type BackendType string

const (
	BackendFilesystem BackendType = "filesystem"
	BackendPostgres   BackendType = "postgres"
)
