#!/bin/bash
set -eu

TIMEZONE=America/New_York

USERNAME=herald

read -p "Enter password for DB user: " DB_PASSWORD

export LC_ALL=en_US.UTF-8 

add-apt-repository --yes universe

apt update

timedatectl set-timezone ${TIMEZONE}
apt --yes install locales-all

useradd --create-home --shell "/bin/bash" --groups sudo "${USERNAME}"

passwd --delete "${USERNAME}"
chage --lastday 0 "${USERNAME}"

rsync --archive --chown=${USERNAME}:${USERNAME} /root/.ssh /home/${USERNAME}

ufw allow 22
ufw allow 80/tcp
ufw allow 443/tcp
ufw --force enable

apt --yes install fail2ban

curl -L https://github.com/golang-migrate/migrate/releases/download/v4.14.1/migrate.linux-amd64.tar.gz | tar xvz
mv migrate.linux-amd64 /usr/local/bin/migrate

# Add PostgreSQL Repository for specific latest version access
apt install -y curl ca-certificates gnupg lsb-release
curl -1sLf https://www.postgresql.org/media/keys/ACCC4CF8.asc | gpg --dearmor | tee /etc/apt/trusted.gpg.d/apt.postgresql.org.gpg > /dev/null
echo "deb http://apt.postgresql.org/pub/repos/apt $(lsb_release -cs)-pgdg main" | tee /etc/apt/sources.list.d/pgdg.list
apt update

# Install PG 18 and pgvector
apt --yes install postgresql-18 postgresql-18-pgvector

sudo -i -u postgres psql -c "CREATE DATABASE poe_herald;"
sudo -i -u postgres psql -d poe_herald -c "CREATE ROLE envoy WITH LOGIN SUPERUSER PASSWORD '${DB_PASSWORD}';"


echo "DB_DSN='postgres://envoy:${DB_PASSWORD}@localhost/poe_herald'" >> /etc/environment

apt --yes install debian-keyring debian-archive-keyring apt-transport-https
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | sudo gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | sudo tee /etc/apt/sources.list.d/caddy-stable.list
apt update
apt --yes install caddy

apt --yes -o Dpkg::Options::="--force-confnew" upgrade

echo "Script complete! Rebooting..."
reboot