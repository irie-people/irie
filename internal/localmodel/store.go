package localmodel

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// StoreModel is a model in Ollama's store, whose weights are a GGUF file
// that llama.cpp's server can run as they are.
type StoreModel struct {
	Name string // Ollama's name for it, such as qwen3.5:9b
	Path string // the blob of its weights
}

// ollamaModelLayer is the media type of a model's weights in an Ollama
// manifest; a vision projector, a template and parameters are other layers.
const ollamaModelLayer = "application/vnd.ollama.image.model"

// OllamaStore lists the models in Ollama's store at dir (ModelsDir). Each
// manifest, at manifests/<registry>/<namespace>/<model>/<tag>, names its
// layers by digest; a layer's blob is blobs/sha256-<hex>. Only the weights
// are listed, so a model runs as text alone. A store that is not there has
// no models.
func OllamaStore(dir string) ([]StoreModel, error) {
	root := filepath.Join(dir, "manifests")
	var out []StoreModel
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == root && os.IsNotExist(err) {
				return fs.SkipAll
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) != 4 {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		var m struct {
			Layers []struct {
				MediaType string `json:"mediaType"`
				Digest    string `json:"digest"`
			} `json:"layers"`
		}
		if json.Unmarshal(b, &m) != nil {
			return nil
		}
		for _, l := range m.Layers {
			hex, ok := strings.CutPrefix(l.Digest, "sha256:")
			if l.MediaType != ollamaModelLayer || !ok || strings.ContainsAny(hex, `/\.`) {
				continue
			}
			out = append(out, StoreModel{Name: ollamaName(parts), Path: filepath.Join(dir, "blobs", "sha256-"+hex)})
			break
		}
		return nil
	})
	return out, err
}

// ollamaName is the name Ollama lists a model by: library models by their
// own name and tag, others with their namespace, and models of another
// registry with that too.
func ollamaName(parts []string) string {
	registry, ns, model, tag := parts[0], parts[1], parts[2], parts[3]
	switch {
	case registry == "registry.ollama.ai" && ns == "library":
		return model + ":" + tag
	case registry == "registry.ollama.ai":
		return ns + "/" + model + ":" + tag
	}
	return registry + "/" + ns + "/" + model + ":" + tag
}
