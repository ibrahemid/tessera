FROM fedora:45
RUN dnf install -y --setopt=install_weak_deps=False \
      dbus-daemon dbus-tools gnome-keyring keyutils xorg-x11-server-Xvfb xclip xsel wl-clipboard sway swaybg grim \
      shadow-utils gzip \
 && dnf clean all \
 && useradd -m -u 1000 tess
