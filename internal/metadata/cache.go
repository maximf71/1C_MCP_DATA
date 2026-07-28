package metadata

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/codex/mcp-1c-data/internal/domain"
)

type CatalogSource interface {
	GetMetadataCatalog(context.Context) (domain.MetadataCatalog, error)
}

type Cache struct {
	mu          sync.Mutex
	source      CatalogSource
	ttl         time.Duration
	now         func() time.Time
	expiresAt   time.Time
	catalog     domain.MetadataCatalog
	fingerprint string
}

func NewCache(source CatalogSource, ttl time.Duration) *Cache {
	return &Cache{source: source, ttl: ttl, now: time.Now}
}

func (c *Cache) Get(ctx context.Context, refresh bool) (domain.MetadataCatalog, string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !refresh && c.fingerprint != "" && c.now().Before(c.expiresAt) {
		return c.catalog, c.fingerprint, nil
	}
	catalog, err := c.source.GetMetadataCatalog(ctx)
	if err != nil {
		return domain.MetadataCatalog{}, "", err
	}
	sort.Slice(catalog.Objects, func(i, j int) bool {
		if catalog.Objects[i].Type == catalog.Objects[j].Type {
			return catalog.Objects[i].Name < catalog.Objects[j].Name
		}
		return catalog.Objects[i].Type < catalog.Objects[j].Type
	})
	raw, err := json.Marshal(catalog)
	if err != nil {
		return domain.MetadataCatalog{}, "", err
	}
	sum := sha256.Sum256(raw)
	c.catalog = catalog
	c.fingerprint = hex.EncodeToString(sum[:])
	c.expiresAt = c.now().Add(c.ttl)
	return c.catalog, c.fingerprint, nil
}

func Search(catalog domain.MetadataCatalog, query string, types []string, field string, limit int) []domain.MetadataObject {
	needle := strings.ToLower(strings.TrimSpace(query))
	fieldNeedle := strings.ToLower(strings.TrimSpace(field))
	typeSet := make(map[string]struct{}, len(types))
	for _, value := range types {
		typeSet[strings.ToLower(strings.TrimSpace(value))] = struct{}{}
	}
	type ranked struct {
		object domain.MetadataObject
		score  int
	}
	rankedObjects := make([]ranked, 0)
	for _, object := range catalog.Objects {
		if !object.CanRead {
			continue
		}
		if len(typeSet) > 0 {
			if _, ok := typeSet[strings.ToLower(object.Type)]; !ok {
				continue
			}
		}
		if fieldNeedle != "" {
			fieldFound := false
			for _, fieldName := range object.Fields {
				if strings.EqualFold(strings.TrimSpace(fieldName), fieldNeedle) {
					fieldFound = true
					break
				}
			}
			if !fieldFound {
				continue
			}
		}
		haystacks := []string{
			strings.ToLower(object.Name),
			strings.ToLower(object.Synonym),
			strings.ToLower(object.QuerySource),
		}
		score := 0
		for index, haystack := range haystacks {
			switch {
			case needle == "":
				score = 1
			case haystack == needle:
				score = max(score, 100-index*5)
			case strings.HasPrefix(haystack, needle):
				score = max(score, 80-index*5)
			case strings.Contains(haystack, needle):
				score = max(score, 60-index*5)
			}
		}
		if score > 0 {
			rankedObjects = append(rankedObjects, ranked{object: object, score: score})
		}
	}
	sort.SliceStable(rankedObjects, func(i, j int) bool {
		if rankedObjects[i].score == rankedObjects[j].score {
			return rankedObjects[i].object.QuerySource < rankedObjects[j].object.QuerySource
		}
		return rankedObjects[i].score > rankedObjects[j].score
	})
	if limit > 0 && len(rankedObjects) > limit {
		rankedObjects = rankedObjects[:limit]
	}
	result := make([]domain.MetadataObject, len(rankedObjects))
	for index := range rankedObjects {
		result[index] = rankedObjects[index].object
	}
	return result
}
