#!/bin/sh

apt-get update
apt-get install -y telnet netcat-openbsd uuid-runtime
rm -rf /var/lib/apt/lists/*

install -D -m 0644 -o root -g root fpm-cron-runner.php /var/wpvip/fpm-cron-runner.php
install -m 0755 -o root -g root cron-runner-postinstall.sh /var/lib/wordpress/postinstall.d/cron-runner-postinstall
rm -rf /var/www/*
ln -sf /wp /var/www/html
