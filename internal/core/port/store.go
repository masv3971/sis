package port

// Store bundles the fixed-shape repositories the application layer depends
// on. It is the port the composition root (internal/adapter) builds from the
// chosen backend (postgres or memory) and hands to downstream consumers so
// they never see concrete adapter types.
//
// Store is a struct, not an interface, because the generic doc-repo factory
// used by the entity services cannot be a method on an interface — Go forbids
// type parameters on interface methods. The composition root therefore keeps
// a private generic helper to build per-entity doc repos; everything else
// goes through Store's fixed fields.
type Store struct {
	Persons  PersonRepository
	Accounts AccountRepository
	Clients  ClientRepository
}
