package service

// PublicationError identifies rejected sources without including their rule bodies,
// URLs, or credentials. Failed compilation never moves the current publication.
type PublicationError struct {
	Message      string
	Exclusions   map[string]string
	SourceErrors map[string]string
}

func (e *PublicationError) Error() string { return e.Message }
