package domain

// ListSpacesInput is a client request to list the project's spaces.
type ListSpacesInput struct {
	Kind  SpaceKind // empty means any
	Limit *int
	After *string // external_id of the last item of the previous page
}

// ListSpacesQuery is a validated ListSpacesInput with defaults resolved.
type ListSpacesQuery struct {
	Kind  SpaceKind
	Limit int
	After *string
}

// Prepare validates the input and resolves defaults.
func (in ListSpacesInput) Prepare() (ListSpacesQuery, error) {
	var v fieldErrors
	q := ListSpacesQuery{Kind: in.Kind, Limit: DefaultListLimit, After: in.After}
	if in.Kind != "" && !in.Kind.Valid() {
		v.add("kind", "valor %q não é aceito", in.Kind)
	}
	if in.Limit != nil {
		q.Limit = *in.Limit
		if q.Limit < 1 || q.Limit > MaxListLimit {
			v.add("limit", "deve estar entre 1 e %d", MaxListLimit)
		}
	}
	return q, v.err()
}

// SpacePage is one page of spaces ordered by external_id. Next is nil on the
// last page.
type SpacePage struct {
	Items []Space
	Next  *string
}
