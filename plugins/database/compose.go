package database

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// GenerateComposeFile creates docker-compose.yml content for the given instance.
func GenerateComposeFile(inst *Instance) string {
	switch inst.Engine {
	case EngineMySQL:
		return generateMySQL(inst)
	case EnginePostgres:
		return generatePostgres(inst)
	case EngineMariaDB:
		return generateMariaDB(inst)
	case EngineRedis:
		return generateRedis(inst)
	default:
		return ""
	}
}

// parseConfig deserializes the JSON config string into EngineConfig.
func parseConfig(configJSON string) *EngineConfig {
	if configJSON == "" {
		return nil
	}
	var cfg EngineConfig
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return nil
	}
	return &cfg
}

// buildMySQLCommand builds the command args for MySQL / MariaDB.
func buildMySQLCommand(cfg *EngineConfig) string {
	if cfg == nil {
		return ""
	}
	var args []string
	if cfg.InnoDBBufferPoolSize != "" {
		args = append(args, fmt.Sprintf("--innodb-buffer-pool-size=%s", cfg.InnoDBBufferPoolSize))
	}
	if cfg.MaxConnections > 0 {
		args = append(args, fmt.Sprintf("--max-connections=%d", cfg.MaxConnections))
	}
	if cfg.CharacterSetServer != "" {
		args = append(args, fmt.Sprintf("--character-set-server=%s", cfg.CharacterSetServer))
	}
	if cfg.CollationServer != "" {
		args = append(args, fmt.Sprintf("--collation-server=%s", cfg.CollationServer))
	}
	if cfg.SlowQueryLog != nil {
		if *cfg.SlowQueryLog {
			args = append(args, "--slow-query-log=ON")
		} else {
			args = append(args, "--slow-query-log=OFF")
		}
	}
	if cfg.LongQueryTime > 0 {
		args = append(args, fmt.Sprintf("--long-query-time=%g", cfg.LongQueryTime))
	}
	if len(args) == 0 {
		return ""
	}
	return strings.Join(args, " ")
}

// buildPostgresCommand builds the command args for PostgreSQL.
func buildPostgresCommand(cfg *EngineConfig) string {
	if cfg == nil {
		return ""
	}
	var args []string
	if cfg.SharedBuffers != "" {
		args = append(args, fmt.Sprintf("-c shared_buffers=%s", cfg.SharedBuffers))
	}
	if cfg.MaxConnections > 0 {
		args = append(args, fmt.Sprintf("-c max_connections=%d", cfg.MaxConnections))
	}
	if cfg.WorkMem != "" {
		args = append(args, fmt.Sprintf("-c work_mem=%s", cfg.WorkMem))
	}
	if cfg.EffectiveCacheSize != "" {
		args = append(args, fmt.Sprintf("-c effective_cache_size=%s", cfg.EffectiveCacheSize))
	}
	if cfg.WalLevel != "" {
		args = append(args, fmt.Sprintf("-c wal_level=%s", cfg.WalLevel))
	}
	if cfg.LogMinDurationStatement != nil {
		args = append(args, fmt.Sprintf("-c log_min_duration_statement=%d", *cfg.LogMinDurationStatement))
	}
	if len(args) == 0 {
		return ""
	}
	return "postgres " + strings.Join(args, " ")
}

// buildRedisCommand builds the full command for Redis (including --requirepass).
func buildRedisCommand(cfg *EngineConfig) string {
	args := []string{"redis-server", `--requirepass "${ROOT_PASSWORD}"`}
	if cfg != nil {
		if cfg.MaxMemory != "" {
			args = append(args, fmt.Sprintf("--maxmemory %s", cfg.MaxMemory))
		}
		if cfg.MaxMemoryPolicy != "" {
			args = append(args, fmt.Sprintf("--maxmemory-policy %s", cfg.MaxMemoryPolicy))
		}
		if cfg.AppendOnly != nil {
			if *cfg.AppendOnly {
				args = append(args, "--appendonly yes")
			} else {
				args = append(args, "--appendonly no")
			}
		}
		if cfg.Save != "" {
			args = append(args, fmt.Sprintf("--save \"%s\"", cfg.Save))
		}
	}
	return strings.Join(args, " ")
}

func generateMySQL(inst *Instance) string {
	cfg := parseConfig(inst.Config)
	cmd := buildMySQLCommand(cfg)

	commandLine := ""
	if cmd != "" {
		commandLine = fmt.Sprintf("    command: >-\n      %s\n", cmd)
	}

	return fmt.Sprintf(`services:
  db:
    image: mysql:%s
    container_name: %s
    restart: unless-stopped
%s    ports:
      - "%d:3306"
    environment:
      MYSQL_ROOT_PASSWORD: "${ROOT_PASSWORD}"
    volumes:
      - db_data:/var/lib/mysql
    deploy:
      resources:
        limits:
          memory: %s
    labels:
      webcasa.plugin: database
      webcasa.instance: "%s"
volumes:
  db_data:
`, inst.Version, inst.ContainerName, commandLine, inst.Port, inst.MemoryLimit, inst.Name)
}

func generatePostgres(inst *Instance) string {
	cfg := parseConfig(inst.Config)
	cmd := buildPostgresCommand(cfg)

	commandLine := ""
	if cmd != "" {
		commandLine = fmt.Sprintf("    command: >-\n      %s\n", cmd)
	}

	// PostgreSQL 18+ changed data directory layout: mount at /var/lib/postgresql
	// instead of /var/lib/postgresql/data. See https://github.com/docker-library/postgres/pull/1259
	volumePath := "/var/lib/postgresql/data"
	if major, err := strconv.Atoi(strings.SplitN(inst.Version, ".", 2)[0]); err == nil && major >= 18 {
		volumePath = "/var/lib/postgresql"
	}

	return fmt.Sprintf(`services:
  db:
    image: postgres:%s
    container_name: %s
    restart: unless-stopped
%s    ports:
      - "%d:5432"
    environment:
      POSTGRES_PASSWORD: "${ROOT_PASSWORD}"
    volumes:
      - db_data:%s
    deploy:
      resources:
        limits:
          memory: %s
    labels:
      webcasa.plugin: database
      webcasa.instance: "%s"
volumes:
  db_data:
`, inst.Version, inst.ContainerName, commandLine, inst.Port, volumePath, inst.MemoryLimit, inst.Name)
}

func generateMariaDB(inst *Instance) string {
	cfg := parseConfig(inst.Config)
	cmd := buildMySQLCommand(cfg) // MariaDB uses the same CLI flags as MySQL

	commandLine := ""
	if cmd != "" {
		commandLine = fmt.Sprintf("    command: >-\n      %s\n", cmd)
	}

	return fmt.Sprintf(`services:
  db:
    image: mariadb:%s
    container_name: %s
    restart: unless-stopped
%s    ports:
      - "%d:3306"
    environment:
      MARIADB_ROOT_PASSWORD: "${ROOT_PASSWORD}"
    volumes:
      - db_data:/var/lib/mysql
    deploy:
      resources:
        limits:
          memory: %s
    labels:
      webcasa.plugin: database
      webcasa.instance: "%s"
volumes:
  db_data:
`, inst.Version, inst.ContainerName, commandLine, inst.Port, inst.MemoryLimit, inst.Name)
}

func generateRedis(inst *Instance) string {
	cfg := parseConfig(inst.Config)
	cmd := buildRedisCommand(cfg)

	return fmt.Sprintf(`services:
  db:
    image: redis:%s
    container_name: %s
    restart: unless-stopped
    ports:
      - "%d:6379"
    command: %s
    volumes:
      - db_data:/data
    deploy:
      resources:
        limits:
          memory: %s
    labels:
      webcasa.plugin: database
      webcasa.instance: "%s"
volumes:
  db_data:
`, inst.Version, inst.ContainerName, inst.Port, cmd, inst.MemoryLimit, inst.Name)
}
