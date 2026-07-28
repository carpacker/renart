package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/bruin-data/bruin/pkg/config"
	"github.com/urfave/cli/v3"
	"golang.org/x/term"

	"renart/internal/clientapi"
	"renart/internal/web/secretstore"
	"renart/internal/web/service"
)

const maxCLISecretBytes = 1024 * 1024

// Secrets manages write-only connection credentials and provides a narrowly
// scoped environment bridge for terminal-first tools. Values are never accepted
// through argv and status output contains metadata only.
func Secrets() *cli.Command {
	stopParsingAfterCommand := 1
	return &cli.Command{
		Name:     "secrets",
		Usage:    "manage connection credentials without exposing their values",
		Category: categoryProject,
		Commands: []*cli.Command{
			{
				Name:      "status",
				Usage:     "show secret health and bindings",
				ArgsUsage: "[connection] [field]",
				Flags: []cli.Flag{
					workspaceFlag(),
					secretsEnvironmentFlag(),
					&cli.BoolFlag{Name: "json", Usage: "emit secret metadata as JSON"},
				},
				Action: secretsStatusAction,
			},
			{
				Name:      "set",
				Usage:     "replace a connection secret or bind it to an environment variable",
				ArgsUsage: "connection field",
				Description: "Without --from-env, reads the new value from a hidden terminal prompt or stdin.\n" +
					"Secret values are never accepted as command-line arguments.",
				Flags: []cli.Flag{
					workspaceFlag(),
					secretsEnvironmentFlag(),
					&cli.StringFlag{
						Name:  "from-env",
						Usage: "bind the field to this existing environment variable instead of storing a value",
					},
					&cli.StringFlag{
						Name:  "store",
						Value: "credential-store",
						Usage: "storage backend for a value: credential-store or vault",
					},
				},
				Action: secretsSetAction,
			},
			{
				Name:  "vault",
				Usage: "set up, unlock, and lock the encrypted local vault",
				Commands: []*cli.Command{
					{
						Name:   "status",
						Usage:  "show encrypted vault state without exposing values",
						Flags:  []cli.Flag{workspaceFlag(), &cli.BoolFlag{Name: "json"}},
						Action: secretsVaultStatusAction,
					},
					{
						Name:   "init",
						Usage:  "create the encrypted vault for this project",
						Flags:  []cli.Flag{workspaceFlag()},
						Action: secretsVaultInitializeAction,
					},
					{
						Name:   "unlock",
						Usage:  "unlock the vault in the running Renart server",
						Flags:  []cli.Flag{workspaceFlag()},
						Action: secretsVaultUnlockAction,
					},
					{
						Name:   "lock",
						Usage:  "lock the vault in the running Renart server",
						Flags:  []cli.Flag{workspaceFlag()},
						Action: secretsVaultLockAction,
					},
					{
						Name:   "change-passphrase",
						Usage:  "change the passphrase of an unlocked vault",
						Flags:  []cli.Flag{workspaceFlag()},
						Action: secretsVaultChangePassphraseAction,
					},
				},
			},
			{
				Name:      "remove",
				Usage:     "remove a connection secret binding and stored local value",
				ArgsUsage: "connection field",
				Flags: []cli.Flag{
					workspaceFlag(),
					secretsEnvironmentFlag(),
					&cli.BoolFlag{
						Name:  "yes",
						Usage: "confirm removal (required when stdin is not an interactive terminal)",
					},
				},
				Action: secretsRemoveAction,
			},
			{
				Name:      "exec",
				Usage:     "run a command with connection secrets in its child environment",
				ArgsUsage: "command [args...]",
				Description: "Resolves the selected environment's connection secrets for one child process.\n" +
					"The parent environment, project files, command arguments, and Renart output are not modified.",
				Flags: []cli.Flag{
					workspaceFlag(),
					secretsEnvironmentFlag(),
				},
				StopOnNthArg: &stopParsingAfterCommand,
				Action:       secretsExecAction,
			},
		},
	}
}

func secretsEnvironmentFlag() *cli.StringFlag {
	return &cli.StringFlag{
		Name:    "env",
		Aliases: []string{"environment"},
		Usage:   "connection environment (default: the selected or default environment)",
	}
}

type cliSecretsConfig struct {
	root           string
	configPath     string
	configService  *service.ConfigService
	secretResolver *secretstore.Resolver
	secretVault    *secretstore.LocalVaultProvider
	config         *config.Config
}

func loadCLISecretsConfig(c *cli.Command, forEditing bool) (cliSecretsConfig, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return cliSecretsConfig{}, err
	}
	root, err := findWorkspaceRoot(c.String("workspace"), cwd)
	if err != nil {
		return cliSecretsConfig{}, cli.Exit(err.Error(), 2)
	}
	configPath := resolveConfigFilePath(root)
	secretVault := secretstore.NewLocalVaultProvider("")
	secretResolver := secretstore.NewDefaultResolverWithLocalVault(secretVault)
	configService := service.NewConfigService(
		root,
		configPath,
		service.WithSecretResolver(secretResolver),
		service.WithSecretVault(secretVault),
	)
	var cfg *config.Config
	if forEditing {
		cfg, configPath, err = configService.LoadForEditing()
	} else {
		cfg, configPath, err = configService.LoadReadOnly()
	}
	if err != nil {
		return cliSecretsConfig{}, fmt.Errorf("load connection configuration: %w", err)
	}
	return cliSecretsConfig{
		root:           root,
		configPath:     configPath,
		configService:  configService,
		secretResolver: secretResolver,
		secretVault:    secretVault,
		config:         cfg,
	}, nil
}

func (c cliSecretsConfig) environment(requested string) (string, error) {
	name := strings.TrimSpace(requested)
	if name == "" {
		name = strings.TrimSpace(c.config.SelectedEnvironmentName)
	}
	if name == "" {
		name = strings.TrimSpace(c.config.DefaultEnvironmentName)
	}
	if name == "" && len(c.config.Environments) == 1 {
		for candidate := range c.config.Environments {
			name = candidate
		}
	}
	if name == "" {
		return "", fmt.Errorf("no connection environment is selected; pass --env")
	}
	if _, found := c.config.Environments[name]; !found {
		available := c.config.GetEnvironmentNames()
		sort.Strings(available)
		if len(available) == 0 {
			return "", fmt.Errorf("connection environment %q does not exist", name)
		}
		return "", fmt.Errorf(
			"connection environment %q does not exist (available: %s)",
			name,
			strings.Join(available, ", "),
		)
	}
	return name, nil
}

type cliSecretsWorkspace struct {
	cliSecretsConfig
	environmentName string
	response        service.WorkspaceConfigResponse
	environment     service.WorkspaceConfigEnvironment
}

func loadCLISecretsWorkspace(c *cli.Command, forEditing bool) (cliSecretsWorkspace, error) {
	base, err := loadCLISecretsConfig(c, forEditing)
	if err != nil {
		return cliSecretsWorkspace{}, err
	}
	environmentName, err := base.environment(c.String("env"))
	if err != nil {
		return cliSecretsWorkspace{}, cli.Exit(err.Error(), 2)
	}
	response := base.configService.BuildResponse(base.configPath, base.config)
	if response.SecretBindingsError != "" {
		return cliSecretsWorkspace{}, fmt.Errorf(
			"load secret bindings: %s",
			response.SecretBindingsError,
		)
	}
	for _, environment := range response.Environments {
		if environment.Name == environmentName {
			return cliSecretsWorkspace{
				cliSecretsConfig: base,
				environmentName:  environmentName,
				response:         response,
				environment:      environment,
			}, nil
		}
	}
	return cliSecretsWorkspace{}, fmt.Errorf(
		"connection environment %q could not be loaded",
		environmentName,
	)
}

func (w cliSecretsWorkspace) connection(name string) (service.WorkspaceConfigConnection, error) {
	name = strings.TrimSpace(name)
	for _, connection := range w.environment.Connections {
		if connection.Name == name {
			return connection, nil
		}
	}
	available := make([]string, 0, len(w.environment.Connections))
	for _, connection := range w.environment.Connections {
		available = append(available, connection.Name)
	}
	sort.Strings(available)
	if len(available) == 0 {
		return service.WorkspaceConfigConnection{}, fmt.Errorf(
			"environment %q has no connections",
			w.environmentName,
		)
	}
	return service.WorkspaceConfigConnection{}, fmt.Errorf(
		"connection %q does not exist in environment %q (available: %s)",
		name,
		w.environmentName,
		strings.Join(available, ", "),
	)
}

func (w cliSecretsWorkspace) secretField(
	connection service.WorkspaceConfigConnection,
	name string,
) (service.WorkspaceConfigSecretField, service.WorkspaceConfigFieldDef, error) {
	descriptor, found := connection.SecretFields[name]
	if !found {
		available := make([]string, 0, len(connection.SecretFields))
		for fieldName := range connection.SecretFields {
			available = append(available, fieldName)
		}
		sort.Strings(available)
		if len(available) == 0 {
			return service.WorkspaceConfigSecretField{}, service.WorkspaceConfigFieldDef{}, fmt.Errorf(
				"connection %q has no secret fields",
				connection.Name,
			)
		}
		return service.WorkspaceConfigSecretField{}, service.WorkspaceConfigFieldDef{}, fmt.Errorf(
			"field %q is not a secret field on connection %q (available: %s)",
			name,
			connection.Name,
			strings.Join(available, ", "),
		)
	}
	for _, connectionType := range w.response.ConnectionTypes {
		if connectionType.TypeName != connection.Type {
			continue
		}
		for _, field := range connectionType.Fields {
			if field.Name == name {
				return descriptor, field, nil
			}
		}
	}
	return descriptor, service.WorkspaceConfigFieldDef{}, fmt.Errorf(
		"secret field metadata for %s.%s is unavailable",
		connection.Name,
		name,
	)
}

type cliSecretStatus struct {
	Connection string `json:"connection"`
	Field      string `json:"field"`
	Status     string `json:"status"`
	Provider   string `json:"provider,omitempty"`
	Reference  string `json:"reference,omitempty"`
	Writable   bool   `json:"writable"`
	Rotatable  bool   `json:"rotatable"`
	Message    string `json:"message,omitempty"`
}

func secretsStatusAction(_ context.Context, c *cli.Command) error {
	if c.Args().Len() > 2 {
		return cli.Exit("status accepts at most a connection and field", 2)
	}
	if c.Args().Get(1) != "" && c.Args().Get(0) == "" {
		return cli.Exit("a field filter requires a connection", 2)
	}
	workspace, err := loadCLISecretsWorkspace(c, false)
	if err != nil {
		return err
	}

	connectionFilter := c.Args().Get(0)
	fieldFilter := c.Args().Get(1)
	if connectionFilter != "" {
		connection, err := workspace.connection(connectionFilter)
		if err != nil {
			return cli.Exit(err.Error(), 2)
		}
		if fieldFilter != "" {
			if _, _, err := workspace.secretField(connection, fieldFilter); err != nil {
				return cli.Exit(err.Error(), 2)
			}
		}
	}

	rows := make([]cliSecretStatus, 0)
	for _, connection := range workspace.environment.Connections {
		if connectionFilter != "" && connection.Name != connectionFilter {
			continue
		}
		fieldNames := make([]string, 0, len(connection.SecretFields))
		for fieldName := range connection.SecretFields {
			fieldNames = append(fieldNames, fieldName)
		}
		sort.Strings(fieldNames)
		for _, fieldName := range fieldNames {
			if fieldFilter != "" && fieldName != fieldFilter {
				continue
			}
			descriptor := connection.SecretFields[fieldName]
			rows = append(rows, cliSecretStatus{
				Connection: connection.Name,
				Field:      fieldName,
				Status:     descriptor.Status,
				Provider:   descriptor.Provider,
				Reference:  descriptor.Reference,
				Writable:   descriptor.Writable,
				Rotatable:  descriptor.Rotatable,
				Message:    descriptor.Message,
			})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Connection != rows[j].Connection {
			return rows[i].Connection < rows[j].Connection
		}
		return rows[i].Field < rows[j].Field
	})

	if c.Bool("json") {
		encoder := json.NewEncoder(c.Writer)
		encoder.SetIndent("", "  ")
		return encoder.Encode(rows)
	}
	if len(rows) == 0 {
		fmt.Fprintf(c.Writer, "No connection secrets in environment %s.\n", workspace.environmentName)
		return nil
	}
	writer := tabwriter.NewWriter(c.Writer, 2, 4, 2, ' ', 0)
	defer writer.Flush()
	fmt.Fprintln(writer, "CONNECTION\tFIELD\tSTATUS\tPROVIDER\tREFERENCE\tDETAILS")
	for _, row := range rows {
		fmt.Fprintf(
			writer,
			"%s\t%s\t%s\t%s\t%s\t%s\n",
			row.Connection,
			row.Field,
			displayCLISecretMetadata(row.Status),
			displayCLISecretMetadata(row.Provider),
			displayCLISecretMetadata(row.Reference),
			displayCLISecretMetadata(row.Message),
		)
	}
	return nil
}

func displayCLISecretMetadata(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

func secretsSetAction(ctx context.Context, c *cli.Command) error {
	if c.Args().Len() != 2 {
		return cli.Exit("set requires a connection and field", 2)
	}
	workspace, err := loadCLISecretsWorkspace(c, true)
	if err != nil {
		return err
	}
	connection, err := workspace.connection(c.Args().Get(0))
	if err != nil {
		return cli.Exit(err.Error(), 2)
	}
	_, field, err := workspace.secretField(connection, c.Args().Get(1))
	if err != nil {
		return cli.Exit(err.Error(), 2)
	}

	change := service.WorkspaceConnectionSecretChange{Action: "replace"}
	fromEnvironment := strings.TrimSpace(c.String("from-env"))
	store := strings.ToLower(strings.TrimSpace(c.String("store")))
	if store != "credential-store" && store != "vault" {
		return cli.Exit("--store must be credential-store or vault", 2)
	}
	if fromEnvironment != "" {
		if store != "credential-store" {
			return cli.Exit("--from-env and --store=vault cannot be used together", 2)
		}
		reference, err := secretstore.ParseRef("env:" + fromEnvironment)
		if err != nil {
			return cli.Exit(err.Error(), 2)
		}
		change.Binding = &service.WorkspaceConnectionSecretBinding{Ref: reference.String()}
	} else {
		if field.IsSensitiveFile {
			return cli.Exit(
				"file credentials currently require --from-env with an environment variable containing the private file path",
				2,
			)
		}
		value, err := readCLISecretValue(c)
		if err != nil {
			return err
		}
		defer clearCLIBytes(value)
		change.Value = string(value)
		if store == "vault" {
			if err := unlockCLILocalVault(ctx, c, workspace.cliSecretsConfig); err != nil {
				return err
			}
			defer workspace.secretVault.LockAll()
			change.Binding = &service.WorkspaceConnectionSecretBinding{Provider: "local-vault"}
		} else {
			change.Binding = &service.WorkspaceConnectionSecretBinding{Provider: "local"}
		}
	}

	_, err = workspace.configService.UpdateConnectionAndPersist(
		ctx,
		service.UpsertWorkspaceConnectionParams{
			EnvironmentName: workspace.environmentName,
			CurrentName:     connection.Name,
			Name:            connection.Name,
			Type:            connection.Type,
			Values:          connection.Values,
			SecretChanges: map[string]service.WorkspaceConnectionSecretChange{
				field.Name: change,
			},
		},
	)
	if err != nil {
		return fmt.Errorf("set %s.%s: %w", connection.Name, field.Name, err)
	}
	if fromEnvironment != "" {
		fmt.Fprintf(
			c.Writer,
			"Bound %s.%s to env:%s in environment %s.\n",
			connection.Name,
			field.Name,
			fromEnvironment,
			workspace.environmentName,
		)
	} else {
		storageLabel := "local credential store"
		if store == "vault" {
			storageLabel = "encrypted local vault"
		}
		fmt.Fprintf(
			c.Writer,
			"Stored %s.%s in the %s for environment %s.\n",
			connection.Name,
			field.Name,
			storageLabel,
			workspace.environmentName,
		)
	}
	return nil
}

func readCLISecretValue(c *cli.Command) ([]byte, error) {
	if terminal, ok := c.Reader.(*os.File); ok && term.IsTerminal(int(terminal.Fd())) {
		fmt.Fprint(c.ErrWriter, "Secret value: ")
		value, err := term.ReadPassword(int(terminal.Fd()))
		fmt.Fprintln(c.ErrWriter)
		if err != nil {
			return nil, fmt.Errorf("read secret value: %w", err)
		}
		if len(value) == 0 {
			return nil, fmt.Errorf("secret value cannot be empty")
		}
		return value, nil
	}

	value, err := io.ReadAll(io.LimitReader(c.Reader, maxCLISecretBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read secret value from stdin: %w", err)
	}
	if len(value) > maxCLISecretBytes {
		clearCLIBytes(value)
		return nil, fmt.Errorf("secret value from stdin exceeds %d bytes", maxCLISecretBytes)
	}
	value = bytes.TrimSuffix(value, []byte{'\n'})
	value = bytes.TrimSuffix(value, []byte{'\r'})
	if len(value) == 0 {
		return nil, fmt.Errorf("secret value cannot be empty")
	}
	return value, nil
}

func secretsRemoveAction(ctx context.Context, c *cli.Command) error {
	if c.Args().Len() != 2 {
		return cli.Exit("remove requires a connection and field", 2)
	}
	workspace, err := loadCLISecretsWorkspace(c, true)
	if err != nil {
		return err
	}
	connection, err := workspace.connection(c.Args().Get(0))
	if err != nil {
		return cli.Exit(err.Error(), 2)
	}
	descriptor, field, err := workspace.secretField(connection, c.Args().Get(1))
	if err != nil {
		return cli.Exit(err.Error(), 2)
	}
	if !c.Bool("yes") {
		confirmed, err := confirmCLISecretRemoval(c, connection.Name, field.Name, workspace.environmentName)
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Fprintln(c.Writer, "Secret removal cancelled.")
			return nil
		}
	}
	if descriptor.Provider == "local-vault" {
		if err := unlockCLILocalVault(ctx, c, workspace.cliSecretsConfig); err != nil {
			return err
		}
		defer workspace.secretVault.LockAll()
	}

	_, err = workspace.configService.UpdateConnectionAndPersist(
		ctx,
		service.UpsertWorkspaceConnectionParams{
			EnvironmentName: workspace.environmentName,
			CurrentName:     connection.Name,
			Name:            connection.Name,
			Type:            connection.Type,
			Values:          connection.Values,
			SecretChanges: map[string]service.WorkspaceConnectionSecretChange{
				field.Name: {Action: "clear"},
			},
		},
	)
	if err != nil {
		return fmt.Errorf("remove %s.%s: %w", connection.Name, field.Name, err)
	}
	fmt.Fprintf(
		c.Writer,
		"Removed %s.%s from environment %s.\n",
		connection.Name,
		field.Name,
		workspace.environmentName,
	)
	return nil
}

func confirmCLISecretRemoval(
	c *cli.Command,
	connection string,
	field string,
	environment string,
) (bool, error) {
	terminal, ok := c.Reader.(*os.File)
	if !ok || !term.IsTerminal(int(terminal.Fd())) {
		return false, cli.Exit("secret removal from a non-interactive session requires --yes", 2)
	}
	fmt.Fprintf(
		c.ErrWriter,
		"Remove %s.%s from environment %s? [y/N] ",
		connection,
		field,
		environment,
	)
	var answer string
	if _, err := fmt.Fscanln(c.Reader, &answer); err != nil {
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		return false, fmt.Errorf("read confirmation: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

func secretsExecAction(ctx context.Context, c *cli.Command) error {
	commandArgs := c.Args().Slice()
	if len(commandArgs) == 0 {
		return cli.Exit("exec requires a command", 2)
	}
	base, err := loadCLISecretsConfig(c, false)
	if err != nil {
		return err
	}
	environmentName, err := base.environment(c.String("env"))
	if err != nil {
		return cli.Exit(err.Error(), 2)
	}
	project := base.configService.ProjectIdentity()
	if err := unlockCLILocalVaultWhenReferenced(
		ctx,
		c,
		base,
		environmentName,
	); err != nil {
		return err
	}
	defer base.secretVault.LockAll()
	factory := service.NewResolvedConnectionFactory(
		base.root,
		base.configPath,
		project.ID,
		base.secretResolver,
	)
	resolved, err := factory.ResolveConfig(
		ctx,
		base.config,
		environmentName,
		secretstore.PurposeCLIExec,
	)
	if err != nil {
		return fmt.Errorf("resolve connection secrets for environment %s: %w", environmentName, err)
	}
	defer resolved.Close(ctx)

	secretEnvironment := resolved.EnvironmentVariables()
	defer clearCLIStringMap(secretEnvironment)
	childEnvironment := overlayCLIEnvironment(os.Environ(), secretEnvironment)
	defer clearCLIStringSlice(childEnvironment)

	command := exec.CommandContext(ctx, commandArgs[0], commandArgs[1:]...)
	command.Stdin = c.Reader
	command.Stdout = c.Writer
	command.Stderr = c.ErrWriter
	command.Env = childEnvironment
	if err := command.Run(); err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			return cli.Exit("", exitError.ExitCode())
		}
		return fmt.Errorf(
			"run %q: %s",
			commandArgs[0],
			resolved.Redactor.Mask(err.Error()),
		)
	}
	return nil
}

type cliVaultStatus struct {
	State       string `json:"state"`
	SecretCount int    `json:"secret_count"`
	Message     string `json:"message,omitempty"`
	Session     string `json:"session"`
}

func secretsVaultStatusAction(ctx context.Context, c *cli.Command) error {
	base, err := loadCLISecretsConfig(c, false)
	if err != nil {
		return err
	}
	projectID := base.configService.ProjectIdentity().ID
	status := base.secretVault.Status(projectID)
	result := cliVaultStatus{
		State:       string(status.State),
		SecretCount: status.SecretCount,
		Message:     status.Message,
		Session:     "this command",
	}
	if client := discoverCLISecretsServer(ctx, base.root); client != nil {
		response, requestErr := client.WorkspaceConfig(ctx)
		if requestErr == nil {
			result.State = response.SecretVault.State
			result.SecretCount = response.SecretVault.SecretCount
			result.Message = response.SecretVault.Message
			result.Session = "running server"
		}
	}
	if c.Bool("json") {
		encoder := json.NewEncoder(c.Writer)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}
	fmt.Fprintf(c.Writer, "Encrypted vault: %s", result.State)
	if result.State == string(secretstore.LocalVaultUnlocked) {
		fmt.Fprintf(c.Writer, " (%d secrets, %s)", result.SecretCount, result.Session)
	}
	fmt.Fprintln(c.Writer)
	if result.Message != "" {
		fmt.Fprintln(c.Writer, result.Message)
	}
	return nil
}

func secretsVaultInitializeAction(ctx context.Context, c *cli.Command) error {
	base, err := loadCLISecretsConfig(c, false)
	if err != nil {
		return err
	}
	passphrase, err := readCLIVaultPassphrase(c, "New vault passphrase: ", true)
	if err != nil {
		return err
	}
	defer clearCLIBytes(passphrase)
	if client := discoverCLISecretsServer(ctx, base.root); client != nil {
		if _, err := client.InitializeLocalVault(ctx, string(passphrase)); err != nil {
			return fmt.Errorf("initialize encrypted vault in running Renart: %w", err)
		}
		fmt.Fprintln(c.Writer, "Encrypted vault initialized and unlocked in the running Renart server.")
		return nil
	}
	projectID := base.configService.ProjectIdentity().ID
	if err := base.secretVault.Initialize(ctx, projectID, passphrase); err != nil {
		return fmt.Errorf("initialize encrypted vault: %w", err)
	}
	base.secretVault.Lock(projectID)
	fmt.Fprintln(c.Writer, "Encrypted vault initialized. Start Renart and unlock it before using stored credentials.")
	return nil
}

func secretsVaultUnlockAction(ctx context.Context, c *cli.Command) error {
	base, err := loadCLISecretsConfig(c, false)
	if err != nil {
		return err
	}
	client := discoverCLISecretsServer(ctx, base.root)
	if client == nil {
		return cli.Exit(
			"no running Renart server was found; start `renart web`, then unlock its vault session",
			2,
		)
	}
	passphrase, err := readCLIVaultPassphrase(c, "Vault passphrase: ", false)
	if err != nil {
		return err
	}
	defer clearCLIBytes(passphrase)
	if _, err := client.UnlockLocalVault(ctx, string(passphrase)); err != nil {
		return fmt.Errorf("unlock encrypted vault: %w", err)
	}
	fmt.Fprintln(c.Writer, "Encrypted vault unlocked in the running Renart server.")
	return nil
}

func secretsVaultLockAction(ctx context.Context, c *cli.Command) error {
	base, err := loadCLISecretsConfig(c, false)
	if err != nil {
		return err
	}
	client := discoverCLISecretsServer(ctx, base.root)
	if client == nil {
		fmt.Fprintln(c.Writer, "No running Renart server was found; the vault is already locked between commands.")
		return nil
	}
	if _, err := client.LockLocalVault(ctx); err != nil {
		return fmt.Errorf("lock encrypted vault: %w", err)
	}
	fmt.Fprintln(c.Writer, "Encrypted vault locked in the running Renart server.")
	return nil
}

func secretsVaultChangePassphraseAction(ctx context.Context, c *cli.Command) error {
	base, err := loadCLISecretsConfig(c, false)
	if err != nil {
		return err
	}
	client := discoverCLISecretsServer(ctx, base.root)
	if client == nil {
		return cli.Exit(
			"no running Renart server was found; unlock the vault in `renart web` before changing its passphrase",
			2,
		)
	}
	passphrase, err := readCLIVaultPassphrase(c, "New vault passphrase: ", true)
	if err != nil {
		return err
	}
	defer clearCLIBytes(passphrase)
	if _, err := client.ChangeLocalVaultPassphrase(ctx, string(passphrase)); err != nil {
		return fmt.Errorf("change encrypted vault passphrase: %w", err)
	}
	fmt.Fprintln(c.Writer, "Encrypted vault passphrase changed.")
	return nil
}

func discoverCLISecretsServer(ctx context.Context, workspaceRoot string) *clientapi.Client {
	client := clientapi.FromEnv()
	if client != nil {
		if _, err := client.Health(ctx); err == nil {
			return client
		}
		return nil
	}
	client, _ = clientapi.Discover(ctx, workspaceRoot)
	return client
}

func unlockCLILocalVault(
	ctx context.Context,
	c *cli.Command,
	base cliSecretsConfig,
) error {
	projectID := base.configService.ProjectIdentity().ID
	switch base.secretVault.Status(projectID).State {
	case secretstore.LocalVaultUnlocked:
		return nil
	case secretstore.LocalVaultUninitialized:
		return cli.Exit(
			"the encrypted vault is not initialized; run `renart secrets vault init` first",
			2,
		)
	case secretstore.LocalVaultUnavailable:
		return errors.New("the encrypted vault is unavailable")
	}
	passphrase, err := readCLIVaultPassphrase(c, "Vault passphrase: ", false)
	if err != nil {
		return err
	}
	defer clearCLIBytes(passphrase)
	if err := base.secretVault.Unlock(ctx, projectID, passphrase); err != nil {
		return fmt.Errorf("unlock encrypted vault: %w", err)
	}
	return nil
}

func unlockCLILocalVaultWhenReferenced(
	ctx context.Context,
	c *cli.Command,
	base cliSecretsConfig,
	environmentName string,
) error {
	manifest, err := secretstore.LoadManifest(filepath.Join(base.root, ".renart", "secrets.yml"))
	if err != nil {
		return err
	}
	environment := manifest.Environments[environmentName]
	for _, fields := range environment.Connections {
		for _, binding := range fields {
			if binding.Reference.Provider == "local-vault" {
				return unlockCLILocalVault(ctx, c, base)
			}
		}
	}
	return nil
}

func readCLIVaultPassphrase(
	c *cli.Command,
	prompt string,
	confirm bool,
) ([]byte, error) {
	if value := os.Getenv("RENART_VAULT_PASSPHRASE"); value != "" {
		return []byte(value), nil
	}
	terminal, ok := c.Reader.(*os.File)
	if !ok || !term.IsTerminal(int(terminal.Fd())) {
		return nil, cli.Exit(
			"vault passphrase input requires an interactive terminal or RENART_VAULT_PASSPHRASE",
			2,
		)
	}
	fmt.Fprint(c.ErrWriter, prompt)
	passphrase, err := term.ReadPassword(int(terminal.Fd()))
	fmt.Fprintln(c.ErrWriter)
	if err != nil {
		return nil, fmt.Errorf("read vault passphrase: %w", err)
	}
	if !confirm {
		return passphrase, nil
	}
	fmt.Fprint(c.ErrWriter, "Confirm vault passphrase: ")
	confirmation, err := term.ReadPassword(int(terminal.Fd()))
	fmt.Fprintln(c.ErrWriter)
	if err != nil {
		clearCLIBytes(passphrase)
		return nil, fmt.Errorf("confirm vault passphrase: %w", err)
	}
	defer clearCLIBytes(confirmation)
	if !bytes.Equal(passphrase, confirmation) {
		clearCLIBytes(passphrase)
		return nil, errors.New("vault passphrases do not match")
	}
	return passphrase, nil
}

func overlayCLIEnvironment(base []string, overlay map[string]string) []string {
	if len(overlay) == 0 {
		return append([]string(nil), base...)
	}
	keys := make([]string, 0, len(overlay))
	for key := range overlay {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	overlaid := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		overlaid[cliEnvironmentKey(key)] = struct{}{}
	}

	result := make([]string, 0, len(base)+len(keys))
	for _, entry := range base {
		key, _, _ := strings.Cut(entry, "=")
		if cliEnvironmentKey(key) == cliEnvironmentKey("RENART_VAULT_PASSPHRASE") {
			continue
		}
		if _, found := overlaid[cliEnvironmentKey(key)]; found {
			continue
		}
		result = append(result, entry)
	}
	for _, key := range keys {
		result = append(result, key+"="+overlay[key])
	}
	return result
}

func cliEnvironmentKey(value string) string {
	if runtime.GOOS == "windows" {
		return strings.ToUpper(value)
	}
	return value
}

func clearCLIBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

func clearCLIStringMap(values map[string]string) {
	for name := range values {
		values[name] = ""
		delete(values, name)
	}
}

func clearCLIStringSlice(values []string) {
	for index := range values {
		values[index] = ""
	}
}
