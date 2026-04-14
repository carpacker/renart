```
bruin --help
NAME:
   bruin - The CLI used for managing Bruin-powered data pipelines

USAGE:
   bruin [global options] [command [command options]]

VERSION:
   v0.11.464

COMMANDS:
   validate         validate the bruin pipeline configuration for all the pipelines in a given directory
   run              run a Bruin pipeline
   render           render a single Bruin SQL asset
   render-ddl       render a single Bruin SQL asset as DDL
   lineage          dump the lineage for a given asset
   clean            clean the temporary artifacts such as logs and uv caches
   format           format given asset definition files
   ai               AI-powered commands for enhancing and analyzing assets
   docs             Display the link to the Bruin documentation or open it in your browser
   init             init a Bruin pipeline
   environments     manage environments defined in .bruin.yml
   connections
   query            Execute a query on a specified connection and retrieve results
   patch
   data-diff, diff  Compares data between two environments or sources. Table names can be provided as 'connection:table' or just 'table' if a default connection is set via --connection flag.
   import
   upgrade, update  Upgrade Bruin CLI to the latest version or a specific version
   mcp              Start MCP server for Cursor IDE integration
   version
   help, h          Shows a list of commands or help for one command

GLOBAL OPTIONS:
   --debug        show debug information (default: false)
   --help, -h     show help
   --version, -v  print the version


bruin validate --help
NAME:
   bruin validate - validate the bruin pipeline configuration for all the pipelines in a given directory

USAGE:
   bruin validate [options] [path to pipelines]

OPTIONS:
   --environment string, -e string, --env string      the environment to use
   --force, -f                                        force the validation even if the environment is a production environment (default: false)
   --output string, -o string                         the output type, possible values are: plain, json
   --exclude-warnings                                 exclude warning validations from the output (default: false)
   --config-file string                               the path to the .bruin.yml file [$BRUIN_CONFIG_FILE]
   --exclude-tag string                               exclude assets with the given tag from the validation
   --var string [ --var string ]                      override pipeline variables with custom values
   --fast                                             run only fast validation rules, excludes some important rules such as query validation (default: false)
   --exclude-paths string [ --exclude-paths string ]  exclude the given list of paths from the folders that are searched during validation
   --full-refresh                                     validate with full refresh mode enabled (default: false)
   --help, -h                                         show help

GLOBAL OPTIONS:
   --debug  show debug information (default: false)


bruin run --help
NAME:
   bruin run - run a Bruin pipeline

USAGE:
   bruin run [options] [path to the task file]

OPTIONS:
   --downstream                                   pass this flag if you'd like to run all the downstream tasks as well (default: false)
   --workers int                                  number of workers to run the tasks in parallel (default: 16)
   --start-date string                            the start date of the range the pipeline will run for in YYYY-MM-DD, YYYY-MM-DD HH:MM:SS or YYYY-MM-DD HH:MM:SS.ffffff format (default: beginning of yesterday, e.g. 2026-02-18 00:00:00.000000) [$BRUIN_START_DATE]
   --end-date string                              the end date of the range the pipeline will run for in YYYY-MM-DD, YYYY-MM-DD HH:MM:SS or YYYY-MM-DD HH:MM:SS.ffffff format (default: end of yesterday, e.g. 2026-02-18 23:59:59.999999) [$BRUIN_END_DATE]
   --environment string, -e string, --env string  the environment to use
   --push-metadata                                push the metadata to the destination database if supports, currently supported: BigQuery (default: false)
   --force, -f                                    force the validation even if the environment is a production environment (default: false)
   --no-log-file                                  do not create a log file for this run (default: false)
   --sensor-mode string                           Set sensor mode: 'skip' to bypass, 'once' to run once, or 'wait' to loop until expected result (default: 'once' (default), 'skip', 'wait')
   --full-refresh, -r                             truncate the table before running (default: false) [$BRUIN_FULL_REFRESH]
   --apply-interval-modifiers                     apply interval modifiers (default: false)
   --use-pip                                      use pip for managing Python dependencies (default: false)
   --continue                                     use continue to run the pipeline from the last failed asset (default: false)
   --tag string, -t string                        pick the assets with the given tag
   --single-check string                          runs a single column or custom check by ID
   --exclude-tag string, -x string                exclude the assets with given tag
   --only string [ --only string ]                limit the types of tasks to run. By default it will run main and checks, while push-metadata is optional if defined in the pipeline definition (default: 'main', 'checks', 'push-metadata')
   --exp-use-winget-for-uv                        use powershell to manage and install uv on windows, on non-windows systems this has no effect. (default: false)
   --debug-ingestr-src string                     Use ingestr from the given path instead of the builtin version.
   --config-file string                           the path to the .bruin.yml file [$BRUIN_CONFIG_FILE]
   --secrets-backend string                       the source of secrets if different from .bruin.yml. Possible values: 'vault', 'doppler', 'aws', 'azure' [$BRUIN_SECRETS_BACKEND]
   --no-validation                                skip validation for this run. (default: false)
   --no-timestamp                                 skip logging timestamps for this run. (default: false)
   --no-color                                     plain log output for this run. (default: false)
   --verbose                                      print verbose output including SQL queries (default: false)
   --var string [ --var string ]                  override pipeline variables with custom values [$BRUIN_VARS]
   --timeout int                                  timeout for the entire pipeline run in seconds (default: 604800)
   --query-annotations string                     JSON string containing annotations to be added as comments to queries. Use 'default' to only include default annotations.
   --help, -h                                     show help

GLOBAL OPTIONS:
   --debug  show debug information (default: false)


bruin render --help
NAME:
   bruin render - render a single Bruin SQL asset

USAGE:
   bruin render [options] [path to the asset definition]

OPTIONS:
   --full-refresh, -r             truncate the table before running (default: false)
   --start-date string            the start date of the range the pipeline will run for in YYYY-MM-DD, YYYY-MM-DD HH:MM:SS or YYYY-MM-DD HH:MM:SS.ffffff format (default: beginning of yesterday, e.g. 2026-02-18 00:00:00.000000) [$BRUIN_START_DATE]
   --end-date string              the end date of the range the pipeline will run for in YYYY-MM-DD, YYYY-MM-DD HH:MM:SS or YYYY-MM-DD HH:MM:SS.ffffff format (default: end of yesterday, e.g. 2026-02-18 23:59:59.999999) [$BRUIN_END_DATE]
   --output string, -o string     output format (json)
   --config-file string           the path to the .bruin.yml file [$BRUIN_CONFIG_FILE]
   --apply-interval-modifiers     applies interval modifiers if flag is given (default: false)
   --var string [ --var string ]  override pipeline variables with custom values
   --raw-query                    output only the raw query (default: false)
   --help, -h                     show help

GLOBAL OPTIONS:
   --debug  show debug information (default: false)


bruin render --help
NAME:
   bruin render - render a single Bruin SQL asset

USAGE:
   bruin render [options] [path to the asset definition]

OPTIONS:
   --full-refresh, -r             truncate the table before running (default: false)
   --start-date string            the start date of the range the pipeline will run for in YYYY-MM-DD, YYYY-MM-DD HH:MM:SS or YYYY-MM-DD HH:MM:SS.ffffff format (default: beginning of yesterday, e.g. 2026-02-18 00:00:00.000000) [$BRUIN_START_DATE]
   --end-date string              the end date of the range the pipeline will run for in YYYY-MM-DD, YYYY-MM-DD HH:MM:SS or YYYY-MM-DD HH:MM:SS.ffffff format (default: end of yesterday, e.g. 2026-02-18 23:59:59.999999) [$BRUIN_END_DATE]
   --output string, -o string     output format (json)
   --config-file string           the path to the .bruin.yml file [$BRUIN_CONFIG_FILE]
   --apply-interval-modifiers     applies interval modifiers if flag is given (default: false)
   --var string [ --var string ]  override pipeline variables with custom values
   --raw-query                    output only the raw query (default: false)
   --help, -h                     show help

GLOBAL OPTIONS:
   --debug  show debug information (default: false)


bruin lineage --help
NAME:
   bruin lineage - dump the lineage for a given asset

USAGE:
   bruin lineage [options] [path to the asset definition]

OPTIONS:
   --full                      display all the upstream and downstream dependencies even if they are not direct dependencies (default: false)
   --output string, -o string  the output type, possible values are: plain, json
   --help, -h                  show help

GLOBAL OPTIONS:
   --debug  show debug information (default: false)


bruin clean --help
NAME:
   bruin clean - clean the temporary artifacts such as logs and uv caches

USAGE:
   bruin clean [options] [path to project root]

OPTIONS:
   --uv-cache, --uv  clean uv caches (default: false)
   --help, -h        show help

GLOBAL OPTIONS:
   --debug  show debug information (default: false)


bruin format --help
NAME:
   bruin format - format given asset definition files

USAGE:
   bruin format [options] [path to project root]

OPTIONS:
   --output string, -o string  the output type, possible values are: plain, json
   --fail-if-changed           fail the command if any of the assets need reformatting (default: false)
   --sqlfluff                  run sqlfluff to format SQL files (default: false)
   --help, -h                  show help

GLOBAL OPTIONS:
   --debug  show debug information (default: false)


bruin ai --help
NAME:
   bruin ai - AI-powered commands for enhancing and analyzing assets

USAGE:
   bruin ai [command [command options]]

COMMANDS:
   enhance  Enhance asset definitions with AI-powered suggestions for metadata, quality checks, and descriptions

OPTIONS:
   --help, -h  show help


bruin docs --help
NAME:
   bruin docs - Display the link to the Bruin documentation or open it in your browser

USAGE:
   bruin docs [options]

OPTIONS:
   --open      Open the documentation in your default web browser (default: false)
   --help, -h  show help

GLOBAL OPTIONS:
   --debug  show debug information (default: false)


bruin init --help
NAME:
   bruin init - init a Bruin pipeline

USAGE:
   bruin init [options] [template name to be used: athena|bronze-silver-postgres|chess|clickhouse|databricks|default|duckdb|duckdb-example|duckdb-lineage|firebase|frankfurter|gorgias|gsheet-bigquery|gsheet-duckdb|notion|nyc-taxi|oracle-duckdb|python|r|redshift|shopify-bigquery|shopify-duckdb|stripe-databricks|zoomcamp] [name of the folder where the pipeline will be created]

OPTIONS:
   --in-place  initializes the template without creating a bruin repository parent folder (default: false)
   --help, -h  show help

GLOBAL OPTIONS:
   --debug  show debug information (default: false)


bruin environments --help
NAME:
   bruin environments - manage environments defined in .bruin.yml

USAGE:
   bruin environments [command [command options]]

COMMANDS:
   list    list environments found in the current repo
   create  create a new environment
   update  update an existing environment
   delete  delete an existing environment
   clone   clone an existing environment

OPTIONS:
   --help, -h  show help


bruin connections --help
NAME:
   bruin connections

USAGE:
   bruin connections [command [command options]]

COMMANDS:
   list    list connections defined in a Bruin project
   add     add a new connection to a Bruin project
   delete  Delete a connection from an environment
   test    Test the validity of a connection in an environment

OPTIONS:
   --help, -h  show help


bruin query --help
NAME:
   bruin query - Execute a query on a specified connection and retrieve results

USAGE:
   bruin query [options]

OPTIONS:
   --connection string, -c string      the name of the connection to use
   --start-date string                 the start date of the range the pipeline will run for in YYYY-MM-DD, YYYY-MM-DD HH:MM:SS or YYYY-MM-DD HH:MM:SS.ffffff format (default: beginning of yesterday, e.g. 2026-02-18 00:00:00.000000) [$BRUIN_START_DATE]
   --end-date string                   the end date of the range the pipeline will run for in YYYY-MM-DD, YYYY-MM-DD HH:MM:SS or YYYY-MM-DD HH:MM:SS.ffffff format (default: end of yesterday, e.g. 2026-02-18 23:59:59.999999) [$BRUIN_END_DATE]
   --query string, -q string           the SQL query to execute
   --limit int, -l int                 limit the number of rows returned (default: 0)
   --output string, -o string          the output type, possible values are: plain, json, csv (default: plain)
   --timeout int, -t int               timeout for query execution in seconds (default: 1000)
   --asset string                      Path to a SQL asset file within a Bruin pipeline. This file should contain the query to be executed.
   --environment string, --env string  Target environment name as defined in .bruin.yml. Specifies the configuration environment for executing the query.
   --export                            export results to a CSV file  (default: false)
   --config-file string                the path to the .bruin.yml file [$BRUIN_CONFIG_FILE]
   --agent-id string                   agent ID to include in query annotations for tracking purposes [$BRUIN_AGENT_ID]
   --help, -h                          show help

GLOBAL OPTIONS:
   --debug  show debug information (default: false)


bruin patch --help
NAME:
   bruin patch

USAGE:
   bruin patch [command [command options]] [path to the asset or pipeline]

COMMANDS:
   fill-asset-dependencies  Fills missing asset dependencies based on the query. Accepts a path to an asset file or a pipeline directory.
   fill-columns-from-db     Fills the asset's columns from the database schema. Accepts a path to an asset file or a pipeline directory.

OPTIONS:
   --help, -h  show help


bruin import --help
NAME:
   bruin import

USAGE:
   bruin import [command [command options]]

COMMANDS:
   database              Import database tables as Bruin assets
   bq-scheduled-queries  Import BigQuery scheduled queries as Bruin assets
   tableau               Import Tableau dashboards and views as Bruin assets

OPTIONS:
   --help, -h  show help
```
