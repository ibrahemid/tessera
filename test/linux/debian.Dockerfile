FROM debian:trixie-20261104-slim
RUN apt-get update \
 && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      dbus gnome-keyring keyutils xvfb xclip xsel wl-clipboard sway swaybg grim \
 && rm -rf /var/lib/apt/lists/* \
 && rm -f /etc/dpkg/dpkg.cfg.d/docker \
 && useradd -m -u 1000 tess
