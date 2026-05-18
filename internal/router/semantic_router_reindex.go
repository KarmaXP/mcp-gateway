package router

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/KarmaXP/mcp-gateway/internal/defaults"
	"github.com/KarmaXP/mcp-gateway/internal/router/index"
	"github.com/KarmaXP/mcp-gateway/internal/router/store"
)

// Reindex embeds tools and upserts vectors for version. It does not update the
// in-memory catalog; the multiplexer must call ApplyCatalog after a successful
// Reindex in the same critical section as its catVer bump.
func (sr *SemanticRouter) Reindex(ctx context.Context, version string, tools []IndexedTool) error {
	if sr == nil || !sr.Enabled() {
		return nil
	}
	if err := sr.validateReindexPreconditions(version); err != nil {
		return err
	}

	sr.reindexMu.Lock()
	defer sr.reindexMu.Unlock()

	_, err, _ := sr.reindexFlight.Do(version, func() (any, error) {
		return nil, sr.reindexLocked(ctx, version, tools)
	})
	return err
}

func (sr *SemanticRouter) reindexLocked(ctx context.Context, version string, tools []IndexedTool) error {
	docs := sr.formatToolDocuments(tools)
	allEmbeddings, err := sr.embedDocumentsInBatches(ctx, docs)
	if err != nil {
		return err
	}

	records, err := sr.toolRecordsFromEmbeddings(version, tools, allEmbeddings)
	if err != nil {
		return err
	}
	if err := sr.st.Upsert(ctx, records); err != nil {
		if delErr := sr.st.DeleteCatalogVersion(ctx, version); delErr != nil {
			slog.WarnContext(ctx, "router rollback delete failed after upsert error", "version", version, "err", delErr)
		}
		return fmt.Errorf("router: upsert: %w", err)
	}

	slog.InfoContext(ctx, "router catalog vectors upserted", "catalog_version", version, "tools", len(tools))
	return nil
}

// ApplyCatalog swaps the in-memory catalog to version/tools. The multiplexer
// must call this in the same critical section as its catVer update so clients
// never observe mux.catVer ahead of (or behind) sr.catalogVer.
func (sr *SemanticRouter) ApplyCatalog(ctx context.Context, version string, tools []IndexedTool) {
	if sr == nil || !sr.Enabled() || version == "" {
		return
	}

	prevVer := sr.CatalogVersion()
	docs := sr.formatToolDocuments(tools)
	sr.replaceCatalogLocked(version, tools, docs)

	if prevVer != "" && prevVer != version && sr.st != nil {
		if err := sr.st.DeleteCatalogVersion(ctx, prevVer); err != nil {
			slog.WarnContext(ctx, "router delete old catalog version failed", "version", prevVer, "err", err)
		}
	}
	slog.InfoContext(ctx, "router catalog applied", "catalog_version", version, "tools", len(tools))
}

func (sr *SemanticRouter) validateReindexPreconditions(version string) error {
	if sr.embedder == nil || sr.st == nil {
		return fmt.Errorf("router: embedder or store nil: %w", errNilDeps)
	}
	if version == "" {
		return fmt.Errorf("router: empty catalog version")
	}
	return nil
}

func (sr *SemanticRouter) formatToolDocuments(tools []IndexedTool) []string {
	docs := make([]string, len(tools))
	for i, ent := range tools {
		docs[i] = index.FormatDocument(ent.ToolRow)
	}
	return docs
}

func (sr *SemanticRouter) embedDocumentsInBatches(ctx context.Context, docs []string) ([][]float32, error) {
	batch := defaults.ReindexEmbedBatchSize
	all := make([][]float32, 0, len(docs))
	for i := 0; i < len(docs); i += batch {
		j := i + batch
		if j > len(docs) {
			j = len(docs)
		}
		chunk := docs[i:j]
		embCtx, cancel := context.WithTimeout(ctx, sr.cfg.EmbedTimeout)
		vecs, err := sr.embedder.Embed(embCtx, chunk)
		cancel()
		if err != nil {
			return nil, fmt.Errorf("router: embed batch: %w", err)
		}
		all = append(all, vecs...)
	}
	if len(all) != len(docs) {
		return nil, fmt.Errorf("router: embed count mismatch")
	}
	return all, nil
}

func (sr *SemanticRouter) toolRecordsFromEmbeddings(version string, tools []IndexedTool, all [][]float32) ([]store.ToolVectorRecord, error) {
	records := make([]store.ToolVectorRecord, 0, len(tools))
	for i, ent := range tools {
		v := all[i]
		if len(v) != sr.dim {
			return nil, fmt.Errorf("router: vector dim %d want %d", len(v), sr.dim)
		}
		store.L2Normalize(v)
		id := fmt.Sprintf("%s::%s", version, ent.ToolRow.Name)
		records = append(records, store.ToolVectorRecord{
			ID:             id,
			Vector:         v,
			ToolName:       ent.ToolRow.Name,
			UpstreamID:     ent.UpstreamID,
			CatalogVersion: version,
		})
	}
	return records, nil
}

func (sr *SemanticRouter) replaceCatalogLocked(version string, tools []IndexedTool, docs []string) {
	sr.mu.Lock()
	defer sr.mu.Unlock()
	sr.catalogVer = version
	sr.catalog = make(map[string]struct{}, len(tools))
	sr.upstreamByTool = make(map[string]string, len(tools))
	sr.toolDoc = make(map[string]string, len(tools))
	for i, ent := range tools {
		sr.catalog[ent.ToolRow.Name] = struct{}{}
		sr.upstreamByTool[ent.ToolRow.Name] = ent.UpstreamID
		sr.toolDoc[ent.ToolRow.Name] = docs[i]
	}
}
