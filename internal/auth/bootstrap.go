package auth

import (
	"context"
	"fmt"
)

type RegistrationClient interface {
	Register(context.Context, string, string) (Identity, error)
}

// Bootstrap returns existing persistent credentials whenever possible. The
// registration token is only consulted when the credentials Secret is empty.
func Bootstrap(ctx context.Context, store CredentialStore, registrar RegistrationClient, registrationToken, clusterName, version string) (identity Identity, registered bool, err error) {
	identity, err = store.Load(ctx)
	if err != nil {
		return Identity{}, false, err
	}
	if identity.Valid() {
		return identity, false, nil
	}
	if registrationToken == "" {
		return Identity{}, false, fmt.Errorf("agent credentials are absent; a registration token is required")
	}
	identity, err = registrar.Register(ctx, clusterName, version)
	if err != nil {
		return Identity{}, false, err
	}
	if identity.AgentID == "" || !identity.Valid() {
		return Identity{}, false, fmt.Errorf("registration response did not include complete agent credentials")
	}
	if err := store.Save(ctx, identity); err != nil {
		return Identity{}, false, err
	}
	return identity, true, nil
}
