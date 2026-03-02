package database

import "fmt"

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

func generateMySQL(inst *Instance) string {
	return fmt.Sprintf(`services:
  db:
    image: mysql:%s
    container_name: %s
    restart: unless-stopped
    ports:
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
`, inst.Version, inst.ContainerName, inst.Port, inst.MemoryLimit, inst.Name)
}

func generatePostgres(inst *Instance) string {
	return fmt.Sprintf(`services:
  db:
    image: postgres:%s
    container_name: %s
    restart: unless-stopped
    ports:
      - "%d:5432"
    environment:
      POSTGRES_PASSWORD: "${ROOT_PASSWORD}"
    volumes:
      - db_data:/var/lib/postgresql/data
    deploy:
      resources:
        limits:
          memory: %s
    labels:
      webcasa.plugin: database
      webcasa.instance: "%s"
volumes:
  db_data:
`, inst.Version, inst.ContainerName, inst.Port, inst.MemoryLimit, inst.Name)
}

func generateMariaDB(inst *Instance) string {
	return fmt.Sprintf(`services:
  db:
    image: mariadb:%s
    container_name: %s
    restart: unless-stopped
    ports:
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
`, inst.Version, inst.ContainerName, inst.Port, inst.MemoryLimit, inst.Name)
}

func generateRedis(inst *Instance) string {
	return fmt.Sprintf(`services:
  db:
    image: redis:%s
    container_name: %s
    restart: unless-stopped
    ports:
      - "%d:6379"
    command: redis-server --requirepass "${ROOT_PASSWORD}"
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
`, inst.Version, inst.ContainerName, inst.Port, inst.MemoryLimit, inst.Name)
}
