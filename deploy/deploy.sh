#!/bin/sh
set -ue

do_deploy(){
    cd /tmp
    rm -rf vpoker
    git clone "$HOME/vpoker"
    cd vpoker
    sudo make service
}

case "${1-}" in
    node)
        do_deploy
        ;;
    *)
        rsync -aP -p ./deploy/deploy.sh nb:/tmp/deploy.sh
        ssh -t nb /tmp/deploy.sh node
        ;;
esac
