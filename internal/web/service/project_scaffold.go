package service

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bruin-data/bruin/pkg/config"
	bruingit "github.com/bruin-data/bruin/pkg/git"
	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/spf13/afero"

	"renart/internal/web/identity"
)

// ProjectTemplateInfo describes one create-project template for the
// onboarding UI. ID is the value the create endpoint accepts as `template`.
type ProjectTemplateInfo struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Description  string   `json:"description"`
	Offline      bool     `json:"offline"`
	PipelineName string   `json:"pipeline_name"`
	AssetNames   []string `json:"asset_names"`
}

type projectTemplate struct {
	info       ProjectTemplateInfo
	duckdbFile string
	files      func() map[string]string
}

const (
	ProjectTemplateEmpty      = "empty"
	ProjectTemplateBare       = "bare"
	ProjectTemplateChessDemo  = "demo:chess"
	ProjectTemplateRetailDemo = "demo:retail"
)

func projectTemplates() []projectTemplate {
	return []projectTemplate{
		{
			info: ProjectTemplateInfo{
				ID:           ProjectTemplateChessDemo,
				Title:        "Chess performance",
				Description:  "Loads profiles and January 2024 games for popular players, then compares their results, ratings, and opening choices in DuckDB.",
				Offline:      false,
				PipelineName: "chess",
				AssetNames: []string{
					"chess.players",
					"chess.games",
					"chess.game_results",
					"chess.player_performance",
					"chess.opening_repertoire",
				},
			},
			// Keep the DuckDB catalog name distinct from the asset schema. A
			// database named chess.duckdb makes chess.games ambiguous to DuckDB
			// because "chess" can refer to either the catalog or the schema.
			duckdbFile: "chess_playground.duckdb",
			files: func() map[string]string {
				return map[string]string{
					"pipeline.yml":                        quickstartPipelineYAML("chess", "duckdb-default"),
					"assets/chess/players.asset.yml":      chessPlayersAPIYAML(),
					"assets/chess/games.asset.yml":        chessGamesAPIYAML(),
					"assets/chess/game_results.sql":       chessGameResultsSQL("chess"),
					"assets/chess/player_performance.sql": chessPlayerPerformanceSQL("chess"),
					"assets/chess/opening_repertoire.sql": chessOpeningRepertoireSQL("chess"),
				}
			},
		},
		{
			info: ProjectTemplateInfo{
				ID:           ProjectTemplateRetailDemo,
				Title:        "Retail analytics",
				Description:  "A small retail warehouse built from bundled sample data — every asset runs fully offline against local DuckDB.",
				Offline:      true,
				PipelineName: "retail",
				AssetNames:   []string{"raw.customers", "raw.orders", "analytics.customer_orders", "analytics.daily_revenue"},
			},
			duckdbFile: "retail.duckdb",
			files: func() map[string]string {
				return map[string]string{
					"pipeline.yml":                         retailPipelineYAML(),
					"assets/raw/customers.sql":             retailRawCustomersSQL(),
					"assets/raw/orders.sql":                retailRawOrdersSQL(),
					"assets/analytics/customer_orders.sql": retailCustomerOrdersSQL(),
					"assets/analytics/daily_revenue.sql":   retailDailyRevenueSQL(),
				}
			},
		},
		{
			info: ProjectTemplateInfo{
				ID:           ProjectTemplateEmpty,
				Title:        "Empty project",
				Description:  "A minimal pipeline with one example SQL asset against local DuckDB.",
				Offline:      true,
				PipelineName: "analytics",
				AssetNames:   []string{"example.hello"},
			},
			duckdbFile: "analytics.duckdb",
			files: func() map[string]string {
				return map[string]string{
					"pipeline.yml":       emptyPipelineYAML(),
					"assets/example.sql": emptyExampleSQL(),
				}
			},
		},
		{
			// The import flow's shell: project identity, config, and git repo
			// with no pipeline — the table import creates the pipeline itself.
			info: ProjectTemplateInfo{
				ID:          ProjectTemplateBare,
				Title:       "Bare project",
				Description: "Project scaffolding only; a pipeline is added by the import step.",
				Offline:     true,
			},
			duckdbFile: "local.duckdb",
			files:      func() map[string]string { return map[string]string{} },
		},
	}
}

type ProjectTemplatesResponse struct {
	Status    string                `json:"status"`
	Templates []ProjectTemplateInfo `json:"templates"`
}

// ProjectTemplates lists the templates the create-project endpoint accepts.
func ProjectTemplates() []ProjectTemplateInfo {
	templates := projectTemplates()
	infos := make([]ProjectTemplateInfo, 0, len(templates))
	for _, tpl := range templates {
		infos = append(infos, tpl.info)
	}
	return infos
}

func projectTemplateByID(id string) (projectTemplate, bool) {
	for _, tpl := range projectTemplates() {
		if tpl.info.ID == strings.TrimSpace(id) {
			return tpl, true
		}
	}
	return projectTemplate{}, false
}

type ScaffoldProjectRequest struct {
	// TargetDir is the absolute project root; created when missing.
	TargetDir string
	// ConfigPath is the .bruin.yml to write connections into; defaults to
	// <TargetDir>/.bruin.yml.
	ConfigPath string
	// Template is one of the ProjectTemplates IDs.
	Template string
	// NewRepository forces `git init` at TargetDir even when an enclosing
	// repository exists (used when creating a brand-new project directory).
	NewRepository bool
}

type ScaffoldProjectResult struct {
	PipelinePath   string   `json:"pipeline_path"`
	PipelineID     string   `json:"pipeline_id"`
	Files          []string `json:"files"`
	GitInitialized bool     `json:"git_initialized"`
}

// ScaffoldProject writes a template's project files into TargetDir: the
// pipeline directory, a DuckDB connection in the workspace config, the
// project identity, and — when no git repository exists yet (or
// NewRepository is set) — `git init` + .gitignore + an initial commit.
func ScaffoldProject(req ScaffoldProjectRequest) (ScaffoldProjectResult, error) {
	tpl, ok := projectTemplateByID(req.Template)
	if !ok {
		return ScaffoldProjectResult{}, fmt.Errorf("unknown project template %q", req.Template)
	}

	target, err := filepath.Abs(strings.TrimSpace(req.TargetDir))
	if err != nil {
		return ScaffoldProjectResult{}, err
	}
	if target == "" || target == string(filepath.Separator) {
		return ScaffoldProjectResult{}, fmt.Errorf("invalid project directory %q", req.TargetDir)
	}

	fs := afero.NewOsFs()
	if err := fs.MkdirAll(target, 0o755); err != nil {
		return ScaffoldProjectResult{}, err
	}

	pipelineDir := filepath.Join(target, tpl.info.PipelineName)
	if tpl.info.PipelineName != "" {
		if exists, statErr := afero.Exists(fs, pipelineDir); statErr != nil {
			return ScaffoldProjectResult{}, statErr
		} else if exists {
			return ScaffoldProjectResult{}, fmt.Errorf("directory %q already exists in the project", tpl.info.PipelineName)
		}
	}

	// Repository first, so the config below lands relative to the right root.
	repo, initialized, err := openOrInitRepository(target, req.NewRepository)
	if err != nil {
		return ScaffoldProjectResult{}, err
	}

	// The server may have seeded a partial .gitignore already (log sinks
	// append their own patterns), so ensure every default pattern instead of
	// writing the file only when missing.
	created := []string{".gitignore"}
	for _, pattern := range strings.Split(strings.TrimSpace(defaultGitignoreContents), "\n") {
		if err := bruingit.EnsureGivenPatternIsInGitignore(fs, target, pattern); err != nil {
			return ScaffoldProjectResult{}, err
		}
	}

	configPath := strings.TrimSpace(req.ConfigPath)
	if configPath == "" {
		configPath = filepath.Join(target, ".bruin.yml")
	}
	configExisted, _ := afero.Exists(fs, configPath)
	if err := ensureScaffoldDuckDBConnection(target, configPath, "duckdb-files/"+tpl.duckdbFile); err != nil {
		return ScaffoldProjectResult{}, err
	}
	if !configExisted {
		if rel, relErr := filepath.Rel(target, configPath); relErr == nil && !strings.HasPrefix(rel, "..") {
			created = append(created, filepath.ToSlash(rel))
		}
	}
	if err := fs.MkdirAll(filepath.Join(target, "duckdb-files"), 0o755); err != nil {
		return ScaffoldProjectResult{}, err
	}

	for relPath, content := range tpl.files() {
		absPath := filepath.Join(pipelineDir, filepath.FromSlash(relPath))
		if err := fs.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
			return ScaffoldProjectResult{}, err
		}
		if err := afero.WriteFile(fs, absPath, []byte(content), 0o644); err != nil {
			return ScaffoldProjectResult{}, err
		}
		created = append(created, filepath.ToSlash(filepath.Join(tpl.info.PipelineName, relPath)))
	}

	if _, err := identity.EnsureProject(fs, filepath.Join(target, ".renart", "project.yml"), filepath.Base(target)); err != nil {
		return ScaffoldProjectResult{}, err
	}
	created = append(created, ".renart/project.yml")
	sort.Strings(created)

	if initialized {
		if err := commitScaffold(repo, created); err != nil {
			return ScaffoldProjectResult{}, err
		}
	}

	return ScaffoldProjectResult{
		PipelinePath:   tpl.info.PipelineName,
		PipelineID:     EncodeID(tpl.info.PipelineName),
		Files:          created,
		GitInitialized: initialized,
	}, nil
}

// openOrInitRepository returns the repository governing target, initializing
// one at target when none exists (or when forceInit asks for a fresh one).
func openOrInitRepository(target string, forceInit bool) (*gogit.Repository, bool, error) {
	if !forceInit {
		repo, err := gogit.PlainOpenWithOptions(target, &gogit.PlainOpenOptions{DetectDotGit: true})
		if err == nil {
			return repo, false, nil
		}
	}

	repo, err := gogit.PlainInitWithOptions(target, &gogit.PlainInitOptions{
		InitOptions: gogit.InitOptions{
			DefaultBranch: plumbing.NewBranchReferenceName("main"),
		},
	})
	if err != nil {
		// A forced init inside an existing repository degrades to opening it.
		repo, openErr := gogit.PlainOpenWithOptions(target, &gogit.PlainOpenOptions{DetectDotGit: true})
		if openErr != nil {
			return nil, false, err
		}
		return repo, false, nil
	}
	return repo, true, nil
}

func commitScaffold(repo *gogit.Repository, paths []string) error {
	worktree, err := repo.Worktree()
	if err != nil {
		return err
	}
	for _, path := range paths {
		if err := worktree.AddWithOptions(&gogit.AddOptions{Path: filepath.ToSlash(path), SkipStatus: true}); err != nil {
			return err
		}
	}
	_, err = worktree.Commit("Initialize renart project", &gogit.CommitOptions{Author: commitAuthor(repo)})
	return err
}

// ensureScaffoldDuckDBConnection makes sure the workspace config has a
// default environment with a `duckdb-default` connection, without touching
// connections a user already configured.
func ensureScaffoldDuckDBConnection(workspaceRoot, configPath, databasePath string) error {
	configService := NewConfigService(workspaceRoot, configPath)
	cfg, _, err := configService.LoadForEditing()
	if err != nil {
		return err
	}

	environmentName := strings.TrimSpace(cfg.DefaultEnvironmentName)
	if environmentName == "" {
		environmentName = "default"
	}
	if _, exists := cfg.Environments[environmentName]; !exists {
		if err := cfg.AddEnvironment(environmentName, ""); err != nil {
			return err
		}
	}
	if strings.TrimSpace(cfg.DefaultEnvironmentName) == "" {
		cfg.DefaultEnvironmentName = environmentName
	}
	if strings.TrimSpace(cfg.SelectedEnvironmentName) == "" {
		cfg.SelectedEnvironmentName = environmentName
	}

	if !workspaceConfigHasConnection(cfg, environmentName, "duckdb-default") {
		if err := cfg.AddConnection(environmentName, "duckdb-default", "duckdb", map[string]any{"path": databasePath}); err != nil {
			return err
		}
	}

	_, err = configService.Persist(cfg)
	return err
}

func workspaceConfigHasConnection(cfg *config.Config, environmentName, connectionName string) bool {
	env, exists := cfg.Environments[environmentName]
	if !exists || env.Connections == nil {
		return false
	}
	for name := range env.Connections.ConnectionsSummaryList() {
		if name == connectionName {
			return true
		}
	}
	return false
}

func retailPipelineYAML() string {
	return `name: retail
schedule: daily
start_date: "2024-01-01"

default_connections:
  duckdb: duckdb-default
`
}

func retailRawCustomersSQL() string {
	return `/* @bruin
name: raw.customers
type: duckdb.sql
materialization:
  type: table
@bruin */

SELECT *
FROM (
    VALUES
    (1, 'Ada Lovelace', 'London', DATE '2023-10-04'),
    (2, 'Grace Hopper', 'New York', DATE '2023-10-19'),
    (3, 'Alan Turing', 'Manchester', DATE '2023-11-02'),
    (4, 'Katherine Johnson', 'Hampton', DATE '2023-11-15'),
    (5, 'Margaret Hamilton', 'Boston', DATE '2023-11-28'),
    (6, 'Claude Shannon', 'Ann Arbor', DATE '2023-12-06'),
    (7, 'Edsger Dijkstra', 'Rotterdam', DATE '2023-12-14'),
    (8, 'Barbara Liskov', 'Los Angeles', DATE '2023-12-22'),
    (9, 'Donald Knuth', 'Milwaukee', DATE '2024-01-03'),
    (10, 'Radia Perlman', 'Portsmouth', DATE '2024-01-11'),
    (11, 'John von Neumann', 'Budapest', DATE '2024-01-18'),
    (12, 'Annie Easley', 'Birmingham', DATE '2024-01-25')
) AS customers (customer_id, customer_name, city, signed_up_at)
`
}

func retailRawOrdersSQL() string {
	return `/* @bruin
name: raw.orders
type: duckdb.sql
materialization:
  type: table
@bruin */

-- Deterministic synthetic orders so the demo runs offline with no seed files.
SELECT
    seq AS order_id,
    1 + (seq * 7) % 12 AS customer_id,
    DATE '2024-01-01' + CAST((seq * 13) % 120 AS INTEGER) AS ordered_at,
    ROUND(4.50 + ((seq * 37) % 9000) / 100.0, 2) AS order_total,
    CASE WHEN (seq * 11) % 10 = 0 THEN 'returned' ELSE 'completed' END AS status
FROM range(1, 481) AS orders (seq)
`
}

func retailCustomerOrdersSQL() string {
	return `/* @bruin
name: analytics.customer_orders
type: duckdb.sql
materialization:
  type: table
depends:
  - raw.customers
  - raw.orders
columns:
  - name: customer_id
    type: integer
    description: Unique customer identifier
    primary_key: true
  - name: lifetime_value
    type: double
    description: Total revenue from the customer's completed orders
@bruin */

SELECT
    customers.customer_id,
    customers.customer_name,
    customers.city,
    count(orders.order_id) AS order_count,
    round(coalesce(sum(orders.order_total) FILTER (WHERE orders.status = 'completed'), 0), 2) AS lifetime_value,
    max(orders.ordered_at) AS last_ordered_at
FROM raw.customers AS customers
LEFT JOIN raw.orders AS orders
    ON orders.customer_id = customers.customer_id
GROUP BY customers.customer_id, customers.customer_name, customers.city
ORDER BY lifetime_value DESC
`
}

func retailDailyRevenueSQL() string {
	return `/* @bruin
name: analytics.daily_revenue
type: duckdb.sql
materialization:
  type: table
depends:
  - raw.orders
@bruin */

SELECT
    ordered_at AS order_date,
    count(*) AS orders,
    round(sum(order_total), 2) AS revenue
FROM raw.orders
WHERE status = 'completed'
GROUP BY ordered_at
ORDER BY ordered_at
`
}

func emptyPipelineYAML() string {
	return `name: analytics
schedule: daily
start_date: "2024-01-01"

default_connections:
  duckdb: duckdb-default
`
}

func emptyExampleSQL() string {
	return `/* @bruin
name: example.hello
type: duckdb.sql
materialization:
  type: table
@bruin */

SELECT
    42 AS answer,
    'hello from renart' AS greeting
`
}
