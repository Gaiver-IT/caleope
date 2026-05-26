#!/bin/bash
# Autocomplétion bash pour la CLI Caleope
# Installé dans /etc/bash_completion.d/caleope par install.sh
# Source manuel : source /etc/bash_completion.d/caleope

_CALEOPE_ROOT="/opt/gaiver-it/caleope"
_CALEOPE_STORE="${_CALEOPE_ROOT}/core/cache/official/apps"
_CALEOPE_INSTALLED="${_CALEOPE_ROOT}/apps-installed"

# Liste les apps disponibles dans le store
_caleope_available_apps() {
    if [[ -d "${_CALEOPE_STORE}" ]]; then
        ls "${_CALEOPE_STORE}" 2>/dev/null
    fi
}

# Liste les apps installées
_caleope_installed_apps() {
    if [[ -d "${_CALEOPE_INSTALLED}" ]]; then
        ls "${_CALEOPE_INSTALLED}" 2>/dev/null
    fi
}

_caleope_complete() {
    local cur prev words cword
    _init_completion 2>/dev/null || {
        COMPREPLY=()
        cur="${COMP_WORDS[COMP_CWORD]}"
        prev="${COMP_WORDS[COMP_CWORD-1]}"
    }

    local commands="install remove list logs restart stop start backup restore update upgrade search status ping"

    # Flags globaux
    local global_flags="--debug --help"

    # Flags par commande
    local install_flags="--domain --param --force --channel"
    local remove_flags="--keep-data"
    local logs_flags="--tail --follow"
    local backup_flags=""
    local restore_flags=""

    # Déterminer la commande en cours (le 2e mot, si présent)
    local cmd=""
    if [[ ${#COMP_WORDS[@]} -ge 2 ]]; then
        cmd="${COMP_WORDS[1]}"
    fi

    case "${prev}" in
        # Après --domain : compléter avec le domaine de base depuis caleope.conf
        --domain)
            local base_domain=""
            if [[ -f "${_CALEOPE_ROOT}/caleope.conf" ]]; then
                base_domain=$(grep '^CALEOPE_DOMAIN=' "${_CALEOPE_ROOT}/caleope.conf" \
                    2>/dev/null | cut -d= -f2)
            fi
            if [[ -n "${base_domain}" ]]; then
                COMPREPLY=($(compgen -W "${base_domain}" -- "${cur}"))
            else
                COMPREPLY=()
            fi
            return
            ;;

        # Après --param : suggestions des paramètres connus
        --param)
            COMPREPLY=($(compgen -W "storage_path=" -- "${cur}"))
            return
            ;;

        # Après --channel
        --channel)
            COMPREPLY=($(compgen -W "stable alpha" -- "${cur}"))
            return
            ;;

        # Après --tail
        --tail)
            COMPREPLY=($(compgen -W "50 100 200 500" -- "${cur}"))
            return
            ;;
    esac

    case "${cmd}" in
        install)
            if [[ "${cur}" == --* ]]; then
                COMPREPLY=($(compgen -W "${install_flags} ${global_flags}" -- "${cur}"))
            else
                # Compléter le nom de l'app (sauf si c'est le flag ou la valeur d'un flag)
                local app_word="${COMP_WORDS[2]:-}"
                if [[ -z "${app_word}" || "${cur}" == "${app_word}" ]]; then
                    COMPREPLY=($(compgen -W "$(_caleope_available_apps)" -- "${cur}"))
                fi
            fi
            ;;

        remove|logs|restart|stop|start|backup)
            if [[ "${cur}" == --* ]]; then
                case "${cmd}" in
                    remove) COMPREPLY=($(compgen -W "${remove_flags} ${global_flags}" -- "${cur}")) ;;
                    logs)   COMPREPLY=($(compgen -W "${logs_flags} ${global_flags}" -- "${cur}")) ;;
                    *)      COMPREPLY=($(compgen -W "${global_flags}" -- "${cur}")) ;;
                esac
            else
                COMPREPLY=($(compgen -W "$(_caleope_installed_apps)" -- "${cur}"))
            fi
            ;;

        restore)
            if [[ "${cur}" == --* ]]; then
                COMPREPLY=($(compgen -W "${global_flags}" -- "${cur}"))
            else
                # Pour restore, compléter d'abord avec les apps installées
                local app_word="${COMP_WORDS[2]:-}"
                if [[ -z "${app_word}" || "${cur}" == "${app_word}" ]]; then
                    COMPREPLY=($(compgen -W "$(_caleope_installed_apps)" -- "${cur}"))
                else
                    # Ensuite avec les backups disponibles pour cette app
                    local backup_dir="${_CALEOPE_ROOT}/backups/${app_word}"
                    if [[ -d "${backup_dir}" ]]; then
                        COMPREPLY=($(compgen -W "$(ls "${backup_dir}" 2>/dev/null)" -- "${cur}"))
                    fi
                fi
            fi
            ;;

        search)
            # Pas de complétion pour la recherche textuelle
            COMPREPLY=()
            ;;

        update|upgrade|list|status|ping)
            if [[ "${cur}" == --* ]]; then
                COMPREPLY=($(compgen -W "${global_flags}" -- "${cur}"))
            fi
            ;;

        ""|caleope)
            # Compléter la commande elle-même
            COMPREPLY=($(compgen -W "${commands}" -- "${cur}"))
            ;;

        *)
            COMPREPLY=($(compgen -W "${commands}" -- "${cur}"))
            ;;
    esac
}

complete -F _caleope_complete caleope
complete -F _caleope_complete caleope-store
