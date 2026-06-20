# Makefile — Automatise la compilation et l'installation de Caleope
#
# UTILISATION :
#   make build      → compiler les binaires
#   make install    → installer sur le système
#   make dev        → lancer le daemon en mode développement
#   make clean      → nettoyer les binaires

# Variables
BINARY_DAEMON  = caleoped
BINARY_CLI     = caleope
BINARY_UI      = caleope-ui
BUILD_DIR      = ./build
INSTALL_DIR    = /usr/local/bin
SERVICE_DIR    = /etc/systemd/system
BASE_DIR       = /opt/gaiver-it/caleope

# Version — lue depuis git tag, fallback sur "dev"
VERSION  ?= $(shell git describe --tags --abbrev=0 2>/dev/null || echo "dev")
COMMIT   ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

# go build flags :
# -ldflags="-s -w" = supprimer les symboles de debug → binaire plus petit
# -X injecte des variables dans le binaire au moment de la compilation
# CGO_ENABLED=0    = pas de dépendances C → binaire 100% statique (portable)
GO_FLAGS = -ldflags="-s -w -X github.com/gaiver-it/caleope/pkg/version.Version=$(VERSION) -X github.com/gaiver-it/caleope/pkg/version.Commit=$(COMMIT)"
GO_ENV   = CGO_ENABLED=0 GOOS=linux GOARCH=amd64

.PHONY: build release install uninstall dev clean help

## build : Compiler les trois binaires (daemon, CLI, UI)
build:
	@echo "🔨 Compilation..."
	@mkdir -p $(BUILD_DIR)
	$(GO_ENV) go build $(GO_FLAGS) -o $(BUILD_DIR)/$(BINARY_DAEMON) ./cmd/caleoped/
	$(GO_ENV) go build $(GO_FLAGS) -o $(BUILD_DIR)/$(BINARY_CLI)    ./cmd/caleope/
	$(GO_ENV) go build $(GO_FLAGS) -o $(BUILD_DIR)/$(BINARY_UI)     ./cmd/caleope-ui/
	@echo "✅ Binaires dans $(BUILD_DIR)/"
	@ls -lh $(BUILD_DIR)/

## install : Installer sur le système (nécessite sudo)
install: build
	@echo "📦 Installation..."

	# Installer les binaires
	install -m 755 $(BUILD_DIR)/$(BINARY_DAEMON) $(INSTALL_DIR)/
	install -m 755 $(BUILD_DIR)/$(BINARY_CLI)    $(INSTALL_DIR)/
	install -m 755 $(BUILD_DIR)/$(BINARY_UI)     $(INSTALL_DIR)/
	
	# Créer la structure de répertoires
	mkdir -p $(BASE_DIR)/{core/{daemon,api,store,runtime,cache},apps-store,apps-installed,app-config,app-data,runtime/{apps,events},backups,logs}
	
	# Installer le service systemd
	install -m 644 caleoped.service $(SERVICE_DIR)/
	systemctl daemon-reload
	
	@echo ""
	@echo "✅ Caleope installé !"
	@echo ""
	@echo "Pour démarrer le daemon :"
	@echo "  sudo systemctl enable --now caleoped"
	@echo ""
	@echo "Pour vérifier :"
	@echo "  systemctl status caleoped"
	@echo "  caleope ping"

## uninstall : Désinstaller complètement
uninstall:
	@echo "🗑️  Désinstallation..."
	-systemctl stop caleoped
	-systemctl stop caleope-ui
	-systemctl disable caleoped
	-systemctl disable caleope-ui
	rm -f $(INSTALL_DIR)/$(BINARY_DAEMON)
	rm -f $(INSTALL_DIR)/$(BINARY_CLI)
	rm -f $(INSTALL_DIR)/$(BINARY_UI)
	rm -f $(SERVICE_DIR)/caleoped.service
	rm -f $(SERVICE_DIR)/caleope-ui.service
	systemctl daemon-reload
	@echo "⚠️  Les données dans $(BASE_DIR)/ sont conservées."
	@echo "   Supprimez manuellement si nécessaire : sudo rm -rf $(BASE_DIR)"

## release : Compiler les binaires prêts pour GitHub Releases
release: build
	@echo "📦 Préparation des binaires pour la release GitHub..."
	@mkdir -p $(BUILD_DIR)/release
	cp $(BUILD_DIR)/$(BINARY_DAEMON) $(BUILD_DIR)/release/$(BINARY_DAEMON)-linux-amd64
	cp $(BUILD_DIR)/$(BINARY_CLI)    $(BUILD_DIR)/release/$(BINARY_CLI)-linux-amd64
	cp $(BUILD_DIR)/$(BINARY_UI)     $(BUILD_DIR)/release/$(BINARY_UI)-linux-amd64
	@echo ""
	@echo "✅ Binaires prêts dans $(BUILD_DIR)/release/ :"
	@ls -lh $(BUILD_DIR)/release/
	@echo ""
	@echo "→ Upload sur GitHub Releases :"
	@echo "  $(BUILD_DIR)/release/$(BINARY_DAEMON)-linux-amd64"
	@echo "  $(BUILD_DIR)/release/$(BINARY_CLI)-linux-amd64"

## dev : Lancer le daemon en mode développement (pas de systemd)
dev: build
	@echo "🚧 Mode développement"
	@echo "   Base dir : /tmp/caleope-dev"
	@echo "   Socket   : /tmp/caleoped-dev.sock"
	@echo ""
	$(BUILD_DIR)/$(BINARY_DAEMON) \
		--base-dir /tmp/caleope-dev \
		--socket   /tmp/caleoped-dev.sock

## dev-cli : Alias pour le CLI en mode dev
dev-cli:
	@CALEOPE_SOCKET=/tmp/caleoped-dev.sock $(BUILD_DIR)/$(BINARY_CLI) $(ARGS)

## test : Lancer les tests Go
test:
	go test ./... -v

## clean : Supprimer les binaires compilés
clean:
	rm -rf $(BUILD_DIR)
	@echo "✓ Nettoyé"

## help : Afficher cette aide
help:
	@echo "Caleope — Commandes disponibles :"
	@grep -E '^## ' Makefile | sed 's/## /  make /'
