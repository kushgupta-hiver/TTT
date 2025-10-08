package infra

type IDGenerator interface {
	NewID() string
}
