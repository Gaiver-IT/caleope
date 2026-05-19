# Télécharger et installer
wget https://go.dev/dl/go1.24.3.linux-amd64.tar.gz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.24.3.linux-amd64.tar.gz

# Rendre Go accessible globalement
sudo ln -sf /usr/local/go/bin/go /usr/bin/go
sudo ln -sf /usr/local/go/bin/gofmt /usr/bin/gofmt

# Vérifier
go version
