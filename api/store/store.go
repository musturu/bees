package store

type Storer interface {
	Connect() error
	Close() error
}
