// cmd/caleoped/main.go
//
// 🏠 LE DAEMON — point d'entrée principal
//
// caleoped est le processus qui tourne en arrière-plan (daemon).
// Il est lancé par systemd au démarrage du système.
//
// CONCEPT : package main et func main()
// En Go, le programme commence toujours dans la fonction main()
// du package main. C'est le point d'entrée, comme int main() en C.
//
// CONCEPT : os.Signal et graceful shutdown
// Un daemon doit s'arrêter proprement quand systemd l'arrête.
// systemd envoie SIGTERM (signal "termine-toi") avant SIGKILL (force quit).
// On intercepte SIGTERM pour fermer proprement (flush logs, libérer socket...).

package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/gaiver-it/caleope/internal/api"
	"github.com/gaiver-it/caleope/internal/backup"
	"github.com/gaiver-it/caleope/internal/docker"
	"github.com/gaiver-it/caleope/internal/events"
	"github.com/gaiver-it/caleope/internal/install"
	"github.com/gaiver-it/caleope/internal/metrics"
	"github.com/gaiver-it/caleope/internal/network"
	"github.com/gaiver-it/caleope/internal/runtime"
	"github.com/gaiver-it/caleope/internal/store"
)

func main() {
	// ── Flags CLI du daemon ──
	// flag.String("nom", "défaut", "description") = argument en ligne de commande
	// Ex: caleoped --base-dir /opt/gaiver-it/caleope
	baseDir    := flag.String("base-dir",  "/opt/gaiver-it/caleope", "Répertoire base Caleope")
	socketPath := flag.String("socket",   "/run/caleoped.sock",    "Chemin du socket UNIX")
	apiPort    := flag.Int("api-port",   8765,                    "Port de l'API REST HTTP")
	flag.Parse()

	fmt.Println("╔══════════════════════════════════╗")
	fmt.Println("║     Caleope Daemon v0.1.0        ║")
	fmt.Println("╚══════════════════════════════════╝")
	fmt.Printf("Base: %s\n\n", *baseDir)

	// ── Vérifier que Docker est disponible ──
	if err := docker.CheckDockerAvailable(); err != nil {
		fmt.Fprintf(os.Stderr, "❌ %v\n", err)
		os.Exit(1)
	}

	// ── Initialiser les composants ──
	// On construit les dépendances manuellement (pas de framework DI).
	// C'est volontaire : Go privilégie la simplicité et l'explicite.
	rt := runtime.NewManager(*baseDir)
	if err := rt.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "❌ Initialisation runtime: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✓ Runtime initialisé")

	st := store.NewStore(*baseDir)
	dc := docker.NewClient()
	em := events.NewEmitter(*baseDir)

	installer := install.NewInstaller(rt, st, dc, em, *baseDir)
	bkp := backup.NewManager(rt, dc, *baseDir)
	col := metrics.NewCollector(rt, *baseDir)
	net := network.NewManager(*baseDir)

	// Endpoint Prometheus sur :9100/metrics (pour Grafana)
	go func() {
		mux := http.NewServeMux()
		mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
			snap, err := col.Collect(false)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			w.Header().Set("Content-Type", "text/plain; version=0.0.4")
			fmt.Fprint(w, metrics.PrometheusText(snap))
		})
		fmt.Println("✓ Prometheus metrics sur :9100/metrics")
		_ = http.ListenAndServe(":9100", mux)
	}()

	server := api.NewServer(*socketPath, rt, st, installer, bkp, dc, col, em, net, *baseDir)

	// API REST HTTP
	go func() {
		if err := server.StartHTTP(*apiPort); err != nil {
			fmt.Fprintf(os.Stderr, "⚠️  API REST: %v\n", err)
		}
	}()

	// ── Gestion des signaux système (graceful shutdown) ──
	// make(chan os.Signal, 1) = créer un canal bufferisé de signaux
	// Un canal (channel) en Go = tuyau de communication entre goroutines
	sigChan := make(chan os.Signal, 1)
	// signal.Notify = "envoie-moi ces signaux dans ce canal"
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

	// Lancer le serveur dans une goroutine (en arrière-plan)
	errChan := make(chan error, 1)
	go func() {
		errChan <- server.Listen()
	}()

	fmt.Println("\n✓ Daemon prêt — en attente de connexions")
	fmt.Println("  Appuyez sur Ctrl+C pour arrêter\n")

	// Attendre soit une erreur, soit un signal d'arrêt
	// select = "attendre le premier canal qui a quelque chose"
	select {
	case err := <-errChan:
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ Erreur serveur: %v\n", err)
			os.Exit(1)
		}
	case sig := <-sigChan:
		fmt.Printf("\n→ Signal reçu (%s), arrêt propre...\n", sig)
	}

	// Nettoyage
	_ = os.Remove(*socketPath)
	fmt.Println("✓ Daemon arrêté")
}
