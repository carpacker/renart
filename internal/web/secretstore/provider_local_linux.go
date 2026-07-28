//go:build linux

package secretstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	secretServiceName                = "org.freedesktop.secrets"
	secretServicePath                = dbus.ObjectPath("/org/freedesktop/secrets")
	secretServiceInterface           = "org.freedesktop.Secret.Service"
	secretServiceCollectionInterface = "org.freedesktop.Secret.Collection"
	secretServiceItemInterface       = "org.freedesktop.Secret.Item"
	secretServicePropertiesInterface = "org.freedesktop.DBus.Properties"
	secretServiceLoginCollection     = dbus.ObjectPath("/org/freedesktop/secrets/collection/login")
	credentialStoreProbeTimeout      = 2 * time.Second
)

func (osCredentialStore) Probe(
	ctx context.Context,
	service string,
	user string,
) (credentialStoreProbeState, error) {
	address := strings.TrimSpace(os.Getenv("DBUS_SESSION_BUS_ADDRESS"))
	if address == "" {
		runtimeDirectory := strings.TrimSpace(os.Getenv("XDG_RUNTIME_DIR"))
		if runtimeDirectory != "" {
			address = "unix:path=" + filepath.Join(runtimeDirectory, "bus")
		}
	}
	if address == "" {
		return credentialStoreProbeUnknown, errors.New("no user D-Bus session is available")
	}

	conn, err := dbus.Connect(address)
	if err != nil {
		return credentialStoreProbeUnknown, err
	}
	defer conn.Close()

	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, credentialStoreProbeTimeout)
	defer cancel()

	serviceObject := conn.Object(secretServiceName, secretServicePath)
	collectionPath, err := findLoginCredentialCollection(ctx, serviceObject)
	if err != nil {
		return credentialStoreProbeUnknown, err
	}
	collection := conn.Object(secretServiceName, collectionPath)

	var lockedProperty dbus.Variant
	if err := collection.CallWithContext(
		ctx,
		secretServicePropertiesInterface+".Get",
		0,
		secretServiceCollectionInterface,
		"Locked",
	).Store(&lockedProperty); err != nil {
		return credentialStoreProbeUnknown, err
	}
	locked, ok := lockedProperty.Value().(bool)
	if !ok {
		return credentialStoreProbeUnknown, errors.New("credential collection returned an invalid locked state")
	}
	if locked {
		return credentialStoreProbePermissionRequired, nil
	}

	var items []dbus.ObjectPath
	if err := collection.CallWithContext(
		ctx,
		secretServiceCollectionInterface+".SearchItems",
		0,
		map[string]string{
			"service":  service,
			"username": user,
		},
	).Store(&items); err != nil {
		return credentialStoreProbeUnknown, err
	}
	if len(items) == 0 {
		return credentialStoreProbeMissing, nil
	}
	item := conn.Object(secretServiceName, items[0])
	if err := item.CallWithContext(
		ctx,
		secretServicePropertiesInterface+".Get",
		0,
		secretServiceItemInterface,
		"Locked",
	).Store(&lockedProperty); err != nil {
		return credentialStoreProbeUnknown, err
	}
	locked, ok = lockedProperty.Value().(bool)
	if !ok {
		return credentialStoreProbeUnknown, errors.New("credential item returned an invalid locked state")
	}
	if locked {
		return credentialStoreProbePermissionRequired, nil
	}
	return credentialStoreProbeConfigured, nil
}

func findLoginCredentialCollection(
	ctx context.Context,
	service dbus.BusObject,
) (dbus.ObjectPath, error) {
	var collectionsProperty dbus.Variant
	if err := service.CallWithContext(
		ctx,
		secretServicePropertiesInterface+".Get",
		0,
		secretServiceInterface,
		"Collections",
	).Store(&collectionsProperty); err != nil {
		return "", err
	}
	collections, ok := collectionsProperty.Value().([]dbus.ObjectPath)
	if !ok {
		return "", errors.New("credential service returned an invalid collection list")
	}
	for _, collection := range collections {
		if collection == secretServiceLoginCollection {
			return collection, nil
		}
	}

	var defaultCollection dbus.ObjectPath
	if err := service.CallWithContext(
		ctx,
		secretServiceInterface+".ReadAlias",
		0,
		"default",
	).Store(&defaultCollection); err != nil {
		return "", err
	}
	if defaultCollection == "" || defaultCollection == dbus.ObjectPath("/") {
		return "", errors.New("credential service has no login or default collection")
	}
	return defaultCollection, nil
}
