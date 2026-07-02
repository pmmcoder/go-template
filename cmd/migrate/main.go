package main

import (
	"errors"
	"flag"
	"fmt"
	"my_project/pkg/config"
	"os"
	"path/filepath"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

const migrationsDir = "./migrations"

func main() {
	config.Init(os.Getenv("ROOT_PATH"))
	var (
		cmd  = flag.String("cmd", "up", "Command: up, down, version, force, create")
		name = flag.String("name", "", "Migration name (required for create)")
	)
	flag.Parse()

	switch *cmd {
	case "create":
		if *name == "" {
			fmt.Println("Error: -name is required")
			os.Exit(1)
		}
		createMigration(*name)

	default:
		// 执行迁移
		runMigration(*cmd)
	}
}

func createMigration(name string) {
	// 确保目录存在
	if err := os.MkdirAll(migrationsDir, 0755); err != nil {
		panic(err)
	}

	// 使用时间戳作为版本号
	now := time.Now()
	timestamp := now.Format("20060102150405")
	filename := fmt.Sprintf("%s_%s", timestamp, name)

	upPath := filepath.Join(migrationsDir, filename+".up.sql")
	downPath := filepath.Join(migrationsDir, filename+".down.sql")

	// 创建 up 文件
	upContent := fmt.Sprintf(`-- Migration: %s
-- Created: %s
-- Direction: UP

BEGIN;

-- Your migration SQL here

COMMIT;
`, name, now.Format("2006-01-02 15:04:05"))

	if err := os.WriteFile(upPath, []byte(upContent), 0644); err != nil {
		panic(err)
	}

	// 创建 down 文件
	downContent := fmt.Sprintf(`-- Migration: %s
-- Created: %s
-- Direction: DOWN

BEGIN;

-- Your rollback SQL here

COMMIT;
`, name, now.Format("2006-01-02 15:04:05"))

	if err := os.WriteFile(downPath, []byte(downContent), 0644); err != nil {
		panic(err)
	}

	fmt.Printf("✅ Migration created:\n")
	fmt.Printf("  📄 %s\n", upPath)
	fmt.Printf("  📄 %s\n", downPath)
	fmt.Printf("\n💡 Next steps:\n")
	fmt.Printf("  1. Edit the SQL files\n")
	fmt.Printf("  2. Run: go run cmd/migrate/main.go -cmd up\n")
}

func runMigration(cmd string) {
	absPath, err := filepath.Abs(migrationsDir)
	if err != nil {
		panic(err)
	}
	m, err := migrate.New(
		fmt.Sprintf("file://%s", absPath),
		config.MConfig.DB.DSN,
	)
	if err != nil {
		panic(err)
	}
	defer m.Close()

	switch cmd {
	case "up":
		if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			panic(err)
		}
		fmt.Println("✅ Migration up completed")

	case "down":
		if err := m.Steps(-1); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			panic(err)
		}
		fmt.Println("✅ Migration down completed")

	case "version":
		v, dirty, err := m.Version()
		if err != nil && !errors.Is(err, migrate.ErrNilVersion) {
			panic(err)
		}
		if errors.Is(err, migrate.ErrNilVersion) {
			fmt.Println("📭 No migrations applied")
		} else {
			fmt.Printf("📍 Version: %d (dirty: %v)\n", v, dirty)
		}

	case "force":
		// force 需要额外参数
		fmt.Println("⚠️  Use 'force' with -ver flag")

	default:
		fmt.Printf("Unknown command: %s\n", cmd)
	}
}
