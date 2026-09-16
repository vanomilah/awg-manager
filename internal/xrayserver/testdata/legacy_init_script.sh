#!/bin/sh
PROCS=xray
ARGS="run -c /opt/etc/xray-cdn/config.json"
DESC=$PROCS
PATH=/opt/sbin:/opt/bin:/opt/usr/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin

case "$1" in
    start)
        echo "Starting $DESC..."
        ;;
    stop)
        echo "Stopping $DESC..."
        ;;
    *)
        echo "Usage: $0 {start|stop|restart|status}"
        exit 1
        ;;
esac
exit 0
