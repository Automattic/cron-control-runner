#!/bin/sh

install -D -m 0644 -o root -g root fpm-cron-runner.php /var/wpvip/fpm-cron-runner.php
install -m 0755 -o root -g root cron-runner-postinstall.sh /var/lib/wordpress/postinstall.d/cron-runner-postinstall
rm -rf /var/www/*
ln -sf /wp /var/www/html
