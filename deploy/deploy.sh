#!/bin/sh
set -ue

do_deploy(){
    cd /tmp
    git clone "$HOME/vpoker"
    cd vpoker
    sudo make service
}

case "${1-}" in
    node)
        do_deploy
        ;;
    *)
        cat "$0" | ssh nb sh -s node
        ;;
esac
