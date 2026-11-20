FROM ubuntu:resolute-20261105.1
RUN apt-get update \
 && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      dbus gnome-keyring keyutils xvfb xclip xsel wl-clipboard sway swaybg grim \
 && rm -rf /var/lib/apt/lists/* \
 && rm -f /etc/dpkg/dpkg.cfg.d/excludes \
 && userdel -r ubuntu \
 && useradd -m -u 1000 tess
