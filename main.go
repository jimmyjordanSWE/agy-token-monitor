package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"agy-token-monitor/internal/cli"
	"agy-token-monitor/internal/config"
	"agy-token-monitor/internal/daemon"
	"agy-token-monitor/internal/db"
)

const version = "1.0.0"

func printUsage() {
	fmt.Printf(`AGY Token Monitor & Handoff System (v%s)

Usage:
  agy-token-monitor <command> [arguments]

Commands:
  daemon      Run the background transcript monitor service
  stats       Inspect token consumption, projects, or active session
  export      Export conversation metrics as JSON or CSV
  version     Display version information

Examples:
  agy-token-monitor daemon
  agy-token-monitor daemon --once
  agy-token-monitor stats
  agy-token-monitor stats -c <conv-id>
  agy-token-monitor stats -p
  agy-token-monitor stats -t 10 --filter-project my-app
  agy-token-monitor export --json --out tokens.json
  agy-token-monitor export --csv -c <conv-id>
`, version)
}

func main() {
	cfg, err := config.DefaultConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving config paths: %v\n", err)
		os.Exit(1)
	}

	if len(os.Args) < 2 {
		// Default action: stats for current active conversation
		database, err := db.OpenDB(cfg.DBPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Database error: %v\n", err)
			os.Exit(1)
		}
		defer database.Close()

		if err := cli.RunStats(cfg, database, cli.StatsOptions{}); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		return
	}

	cmd := os.Args[1]

	switch cmd {
	case "daemon":
		runDaemon(cfg, os.Args[2:])
	case "stats":
		runStats(cfg, os.Args[2:])
	case "export":
		runExport(cfg, os.Args[2:])
	case "version", "--version", "-v":
		fmt.Printf("agy-token-monitor version %s (Go 1.27)\n", version)
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func runDaemon(cfg *config.Config, args []string) {
	fs := flag.NewFlagSet("daemon", flag.ExitOnError)
	once := fs.Bool("once", false, "Run single-pass synchronization and exit")
	_ = fs.Parse(args)

	database, err := db.InitDB(cfg.DBPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize database: %v\n", err)
		os.Exit(1)
	}
	defer database.Close()

	d, err := daemon.New(cfg, database)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize daemon: %v\n", err)
		os.Exit(1)
	}
	defer d.Close()

	if err := d.Run(context.Background(), *once); err != nil {
		fmt.Fprintf(os.Stderr, "Daemon error: %v\n", err)
		os.Exit(1)
	}
}

func runStats(cfg *config.Config, args []string) {
	fs := flag.NewFlagSet("stats", flag.ExitOnError)
	convID := fs.String("c", "", "Specific conversation ID (defaults to active)")
	fs.StringVar(convID, "conversation", "", "Specific conversation ID (defaults to active)")
	projects := fs.Bool("p", false, "List token usage aggregated by project/workspace")
	fs.BoolVar(projects, "projects", false, "List token usage aggregated by project/workspace")
	top := fs.Int("t", 0, "Show top N conversations by token consumption")
	fs.IntVar(top, "top", 0, "Show top N conversations by token consumption")
	filterProj := fs.String("filter-project", "", "Filter top conversations by project name")
	query := fs.String("q", "", "Execute raw SQL query on token_metrics.db")
	fs.StringVar(query, "query", "", "Execute raw SQL query on token_metrics.db")

	_ = fs.Parse(args)

	database, err := db.OpenDB(cfg.DBPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Database error: %v\n", err)
		os.Exit(1)
	}
	defer database.Close()

	opts := cli.StatsOptions{
		ConversationID: *convID,
		Projects:       *projects,
		Top:            *top,
		FilterProject:  *filterProj,
		Query:          *query,
	}

	if err := cli.RunStats(cfg, database, opts); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func runExport(cfg *config.Config, args []string) {
	fs := flag.NewFlagSet("export", flag.ExitOnError)
	jsonFmt := fs.Bool("json", false, "Export in JSON format")
	csvFmt := fs.Bool("csv", false, "Export in CSV format")
	outPath := fs.String("out", "", "Output file path (default stdout)")
	convID := fs.String("c", "", "Specific conversation ID to export")
	fs.StringVar(convID, "conversation", "", "Specific conversation ID to export")

	_ = fs.Parse(args)

	fmtChoice := "json"
	if *csvFmt {
		fmtChoice = "csv"
	} else if *jsonFmt {
		fmtChoice = "json"
	}

	database, err := db.OpenDB(cfg.DBPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Database error: %v\n", err)
		os.Exit(1)
	}
	defer database.Close()

	opts := cli.ExportOptions{
		Format:         fmtChoice,
		OutPath:        *outPath,
		ConversationID: *convID,
	}

	if err := cli.RunExport(cfg, database, opts); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}
