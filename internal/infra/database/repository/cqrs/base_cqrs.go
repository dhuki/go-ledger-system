package cqrs

type DB interface {
	Query
	Command
}
