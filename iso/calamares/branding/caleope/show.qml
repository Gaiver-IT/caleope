/* Slideshow Caleope affiché pendant l'installation (Calamares slideshowAPI 2). */
import QtQuick 2.0
import calamares.slideshow 1.0

Presentation {
    id: presentation

    function onActivate()   { presentation.startAutoAdvance(8000); }
    function onLeave()      { presentation.stopAutoAdvance(); }

    Slide {
        Rectangle {
            anchors.fill: parent
            color: "#0c0c0e"
            Column {
                anchors.centerIn: parent
                spacing: 14
                Text {
                    text: "CALEOPE"
                    color: "#ffffff"; font.pixelSize: 46; font.bold: true
                    anchors.horizontalCenter: parent.horizontalCenter
                }
                Text {
                    text: "Ton cloud personnel, tes données, ton serveur."
                    color: "#a0a0aa"; font.pixelSize: 18
                    anchors.horizontalCenter: parent.horizontalCenter
                }
            }
        }
    }

    Slide {
        Rectangle {
            anchors.fill: parent
            color: "#0c0c0e"
            Column {
                anchors.centerIn: parent; spacing: 10; width: parent.width * 0.7
                Text { text: "Installation en cours…"; color: "#7c6cff"; font.pixelSize: 26; font.bold: true }
                Text {
                    text: "Debian + Docker + Caleope sont configurés automatiquement. "
                        + "Dans un instant tu pourras installer des dizaines d'apps "
                        + "(Immich, Jellyfin, Nextcloud, Vaultwarden…) en un clic."
                    color: "#e8e8ec"; font.pixelSize: 16; wrapMode: Text.WordWrap; width: parent.width
                }
            }
        }
    }

    Slide {
        Rectangle {
            anchors.fill: parent
            color: "#0c0c0e"
            Column {
                anchors.centerIn: parent; spacing: 10; width: parent.width * 0.7
                Text { text: "Presque prêt"; color: "#7c6cff"; font.pixelSize: 26; font.bold: true }
                Text {
                    text: "Au redémarrage, un assistant (à l'écran ou via ton navigateur sur "
                        + "http://<IP>:8766) te demandera ton domaine et tes mots de passe. "
                        + "C'est tout — Caleope s'occupe du reste."
                    color: "#e8e8ec"; font.pixelSize: 16; wrapMode: Text.WordWrap; width: parent.width
                }
            }
        }
    }
}
