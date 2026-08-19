// pkg/version/version.go
//
// 🏷️ VERSION — injectée à la compilation
//
// Ces variables sont vides par défaut.
// Le Makefile les remplit via -ldflags au moment du build :
//   -X github.com/gaiver-it/caleope/pkg/version.Version=v0.2.0
//   -X github.com/gaiver-it/caleope/pkg/version.Commit=abc1234
//
// Résultat : le binaire connaît sa propre version sans fichier externe.

package version

var (
	Version = "v0.9.20"  // ex: v0.2.0 — injecté par le Makefile
	Commit  = "unknown" // ex: abc1234 — hash git court
)
