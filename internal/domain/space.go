package domain

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// SpaceKind classifies a memory space.
type SpaceKind string

const (
	SpaceKindUser    SpaceKind = "user"
	SpaceKindAgent   SpaceKind = "agent"
	SpaceKindTeam    SpaceKind = "team"
	SpaceKindSession SpaceKind = "session"
	SpaceKindCustom  SpaceKind = "custom"
)

// DefaultSpaceKind is used when the client does not choose a kind.
const DefaultSpaceKind = SpaceKindCustom

// AllSpaceKinds lists every accepted kind.
var AllSpaceKinds = []SpaceKind{SpaceKindUser, SpaceKindAgent, SpaceKindTeam, SpaceKindSession, SpaceKindCustom}

// Valid reports whether k is an accepted kind.
func (k SpaceKind) Valid() bool {
	switch k {
	case SpaceKindUser, SpaceKindAgent, SpaceKindTeam, SpaceKindSession, SpaceKindCustom:
		return true
	}
	return false
}

// MaxExternalIDLength is the largest external_id, in characters.
const MaxExternalIDLength = 255

// Space is an isolated memory container inside a project, addressed by the
// client through ExternalID.
type Space struct {
	ID         uuid.UUID
	ProjectID  uuid.UUID
	ExternalID string
	Name       string
	Kind       SpaceKind
	Metadata   Metadata
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// SpaceInput is the client request to create or update a space.
type SpaceInput struct {
	ExternalID string
	Name       string
	Kind       SpaceKind
	Metadata   Metadata
}

// Prepare validates the input and fills defaults.
func (in SpaceInput) Prepare() (SpaceInput, error) {
	var v fieldErrors
	validateExternalID(&v, in.ExternalID)
	if strings.TrimSpace(in.Name) == "" {
		v.add("name", "é obrigatório")
	}
	if in.Kind == "" {
		in.Kind = DefaultSpaceKind
	} else if !in.Kind.Valid() {
		v.add("kind", "valor %q não é aceito", in.Kind)
	}
	in.Metadata = in.Metadata.orEmpty()
	validateMetadata(&v, "metadata", in.Metadata)
	return in, v.err()
}

// ValidateExternalID checks the identifier used in /v1/spaces/{space}.
func ValidateExternalID(id string) error {
	var v fieldErrors
	validateExternalID(&v, id)
	return v.err()
}

func validateExternalID(v *fieldErrors, id string) {
	switch {
	case strings.TrimSpace(id) == "":
		v.add("space", "é obrigatório")
	case utf8.RuneCountInString(id) > MaxExternalIDLength:
		v.add("space", "excede %d caracteres", MaxExternalIDLength)
	}
}
