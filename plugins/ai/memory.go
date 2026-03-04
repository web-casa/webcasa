package ai

import (
	"encoding/binary"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"
	"unicode"

	"gorm.io/gorm"
)

// Memory represents a persistent fact extracted from AI conversations.
type Memory struct {
	ID             uint           `json:"id" gorm:"primaryKey"`
	Content        string         `json:"content" gorm:"type:text;not null"`
	Category       string         `json:"category" gorm:"index;default:'general'"`
	Importance     float32        `json:"importance" gorm:"default:0.5"`
	Embedding      []byte         `json:"-" gorm:"type:blob"`
	EmbeddingModel string         `json:"-" gorm:"type:text"`
	SourceConvID   *uint          `json:"source_conv_id,omitempty" gorm:"index"`
	AccessCount    int            `json:"access_count" gorm:"default:0"`
	LastAccessedAt *time.Time     `json:"last_accessed_at"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	DeletedAt      gorm.DeletedAt `json:"-" gorm:"index"`
}

func (Memory) TableName() string { return "plugin_ai_memories" }

// MemoryService manages persistent AI memories.
type MemoryService struct {
	db              *gorm.DB
	embeddingClient *EmbeddingClient
	logger          *slog.Logger
}

// NewMemoryService creates a new memory service.
func NewMemoryService(db *gorm.DB, logger *slog.Logger) *MemoryService {
	return &MemoryService{db: db, logger: logger}
}

// SetEmbeddingClient sets the embedding client for vector search.
func (ms *MemoryService) SetEmbeddingClient(client *EmbeddingClient) {
	ms.embeddingClient = client
}

// SaveMemory stores a new memory with deduplication.
func (ms *MemoryService) SaveMemory(content, category string, importance float32, sourceConvID *uint) (*Memory, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, fmt.Errorf("empty memory content")
	}
	if category == "" {
		category = "general"
	}
	if importance < 0 {
		importance = 0
	}
	if importance > 1 {
		importance = 1
	}

	// Generate embedding if client available.
	var embeddingBytes []byte
	var embModel string
	if ms.embeddingClient != nil {
		vec, err := ms.embeddingClient.Embed(content)
		if err != nil {
			ms.logger.Warn("failed to generate embedding, saving without", "err", err)
		} else {
			embeddingBytes = serializeEmbedding(vec)
			embModel = ms.embeddingClient.model
		}
	}

	// Deduplicate: check if a very similar memory already exists.
	if dup, err := ms.findDuplicate(content, embeddingBytes); err == nil && dup != nil {
		// Update the existing memory instead of creating a duplicate.
		dup.UpdatedAt = time.Now()
		dup.AccessCount++
		if importance > dup.Importance {
			dup.Importance = importance
		}
		ms.db.Save(dup)
		return dup, nil
	}

	// Prune if over limit.
	ms.pruneIfNeeded()

	mem := &Memory{
		Content:        content,
		Category:       category,
		Importance:     importance,
		Embedding:      embeddingBytes,
		EmbeddingModel: embModel,
		SourceConvID:   sourceConvID,
	}
	if err := ms.db.Create(mem).Error; err != nil {
		return nil, fmt.Errorf("create memory: %w", err)
	}
	return mem, nil
}

// SearchMemories finds the most relevant memories for a query.
func (ms *MemoryService) SearchMemories(query string, topK int) ([]Memory, error) {
	if topK <= 0 {
		topK = 8
	}

	// Try vector search first.
	if ms.embeddingClient != nil {
		queryVec, err := ms.embeddingClient.Embed(query)
		if err == nil && queryVec != nil {
			return ms.searchByVector(queryVec, topK)
		}
		ms.logger.Warn("embedding failed, falling back to keyword search", "err", err)
	}

	return ms.SearchByKeyword(query, topK)
}

// searchByVector performs cosine similarity search across all memories.
func (ms *MemoryService) searchByVector(queryVec []float32, topK int) ([]Memory, error) {
	var memories []Memory
	if err := ms.db.Where("embedding IS NOT NULL AND length(embedding) > 0").Find(&memories).Error; err != nil {
		return nil, err
	}

	type scored struct {
		mem   Memory
		score float32
	}
	var results []scored
	for _, m := range memories {
		vec := deserializeEmbedding(m.Embedding)
		if len(vec) == 0 {
			continue
		}
		sim := cosineSimilarity(queryVec, vec)
		if sim > 0.3 { // minimum relevance threshold
			results = append(results, scored{mem: m, score: sim})
		}
	}

	// Sort by score descending (simple insertion sort for small N).
	for i := 1; i < len(results); i++ {
		for j := i; j > 0 && results[j].score > results[j-1].score; j-- {
			results[j], results[j-1] = results[j-1], results[j]
		}
	}

	if len(results) > topK {
		results = results[:topK]
	}

	// Update access stats and collect results.
	now := time.Now()
	out := make([]Memory, len(results))
	for i, r := range results {
		out[i] = r.mem
		out[i].Embedding = nil // don't return embedding bytes
		ms.db.Model(&Memory{}).Where("id = ?", r.mem.ID).Updates(map[string]interface{}{
			"access_count":    gorm.Expr("access_count + 1"),
			"last_accessed_at": now,
		})
	}
	return out, nil
}

// SearchByKeyword performs keyword-based search as fallback.
func (ms *MemoryService) SearchByKeyword(query string, topK int) ([]Memory, error) {
	if topK <= 0 {
		topK = 8
	}

	keywords := tokenize(query)
	if len(keywords) == 0 {
		// Return most important recent memories.
		var memories []Memory
		ms.db.Order("importance DESC, updated_at DESC").Limit(topK).Find(&memories)
		return memories, nil
	}

	// Search with OR conditions on keywords.
	var memories []Memory
	tx := ms.db.Model(&Memory{})
	for _, kw := range keywords {
		tx = tx.Or("content LIKE ?", "%"+kw+"%")
	}
	if err := tx.Order("importance DESC, updated_at DESC").Limit(topK * 2).Find(&memories).Error; err != nil {
		return nil, err
	}

	// Score by keyword match count.
	type scored struct {
		mem   Memory
		score int
	}
	var results []scored
	for _, m := range memories {
		lower := strings.ToLower(m.Content)
		score := 0
		for _, kw := range keywords {
			if strings.Contains(lower, kw) {
				score++
			}
		}
		if score > 0 {
			results = append(results, scored{mem: m, score: score})
		}
	}

	// Sort by score descending.
	for i := 1; i < len(results); i++ {
		for j := i; j > 0 && results[j].score > results[j-1].score; j-- {
			results[j], results[j-1] = results[j-1], results[j]
		}
	}

	if len(results) > topK {
		results = results[:topK]
	}

	now := time.Now()
	out := make([]Memory, len(results))
	for i, r := range results {
		out[i] = r.mem
		out[i].Embedding = nil
		ms.db.Model(&Memory{}).Where("id = ?", r.mem.ID).Updates(map[string]interface{}{
			"access_count":    gorm.Expr("access_count + 1"),
			"last_accessed_at": now,
		})
	}
	return out, nil
}

// BuildMemoryContext builds a formatted string for system prompt injection.
func (ms *MemoryService) BuildMemoryContext(query string, maxMemories int) (string, error) {
	memories, err := ms.SearchMemories(query, maxMemories)
	if err != nil || len(memories) == 0 {
		return "", err
	}

	var sb strings.Builder
	sb.WriteString("## Server Memory (from previous interactions)\n")
	for _, m := range memories {
		sb.WriteString(fmt.Sprintf("- [%s] %s\n", m.Category, m.Content))
	}
	return sb.String(), nil
}

// ListMemories returns paginated memories.
func (ms *MemoryService) ListMemories(page, pageSize int, category string) ([]Memory, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}

	var total int64
	tx := ms.db.Model(&Memory{})
	if category != "" {
		tx = tx.Where("category = ?", category)
	}
	tx.Count(&total)

	var memories []Memory
	err := tx.Order("updated_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&memories).Error
	// Clear embedding bytes from response.
	for i := range memories {
		memories[i].Embedding = nil
	}
	return memories, total, err
}

// DeleteMemory removes a memory by ID.
func (ms *MemoryService) DeleteMemory(id uint) error {
	return ms.db.Delete(&Memory{}, id).Error
}

// ClearAll removes all memories.
func (ms *MemoryService) ClearAll() error {
	return ms.db.Where("1 = 1").Delete(&Memory{}).Error
}

// Count returns the total number of memories.
func (ms *MemoryService) Count() (int64, error) {
	var count int64
	err := ms.db.Model(&Memory{}).Count(&count).Error
	return count, err
}

// findDuplicate checks if a very similar memory already exists.
func (ms *MemoryService) findDuplicate(content string, embeddingBytes []byte) (*Memory, error) {
	// Try vector dedup first.
	if len(embeddingBytes) > 0 {
		queryVec := deserializeEmbedding(embeddingBytes)
		var memories []Memory
		if err := ms.db.Where("embedding IS NOT NULL AND length(embedding) > 0").Find(&memories).Error; err != nil {
			return nil, err
		}
		for _, m := range memories {
			vec := deserializeEmbedding(m.Embedding)
			if cosineSimilarity(queryVec, vec) > 0.92 {
				return &m, nil
			}
		}
		return nil, nil
	}

	// Fallback: exact substring check.
	lower := strings.ToLower(strings.TrimSpace(content))
	var memories []Memory
	ms.db.Where("LOWER(content) = ?", lower).Limit(1).Find(&memories)
	if len(memories) > 0 {
		return &memories[0], nil
	}
	return nil, nil
}

// pruneIfNeeded removes low-value memories if over the limit.
func (ms *MemoryService) pruneIfNeeded() {
	const maxMemories = 5000
	count, _ := ms.Count()
	if count < maxMemories {
		return
	}

	// Delete lowest-scoring memories (importance * log(accessCount+1)).
	excess := int(count - maxMemories + 100) // remove 100 extra for headroom
	if excess <= 0 {
		return
	}

	// Simple approach: delete oldest with lowest importance.
	ms.db.Where("id IN (?)",
		ms.db.Model(&Memory{}).Select("id").Order("importance ASC, access_count ASC, updated_at ASC").Limit(excess),
	).Delete(&Memory{})
}

// ── Vector helpers ──

// cosineSimilarity computes cosine similarity between two float32 slices.
func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float32
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (float32(math.Sqrt(float64(normA))) * float32(math.Sqrt(float64(normB))))
}

// serializeEmbedding converts []float32 to []byte (little-endian).
func serializeEmbedding(vec []float32) []byte {
	buf := make([]byte, len(vec)*4)
	for i, v := range vec {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	return buf
}

// deserializeEmbedding converts []byte back to []float32.
func deserializeEmbedding(data []byte) []float32 {
	if len(data)%4 != 0 {
		return nil
	}
	vec := make([]float32, len(data)/4)
	for i := range vec {
		vec[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4:]))
	}
	return vec
}

// tokenize splits a query into lowercase keywords for search.
func tokenize(text string) []string {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	// Filter out very short words.
	var result []string
	for _, w := range words {
		if len(w) >= 2 {
			result = append(result, w)
		}
	}
	if len(result) > 10 {
		result = result[:10]
	}
	return result
}
