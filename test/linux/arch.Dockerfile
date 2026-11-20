FROM archlinux:base-20261108.0.623074
# pacman's seccomp sandbox fails with EINVAL when the image runs under amd64 emulation
RUN sed -i 's/^#DisableSandboxSyscalls/DisableSandboxSyscalls/' /etc/pacman.conf \
 && pacman -Syu --noconfirm --needed \
      dbus gnome-keyring keyutils xorg-server-xvfb xclip xsel wl-clipboard sway swaybg grim \
 && pacman -Scc --noconfirm \
 && sed -i '/^NoExtract/d' /etc/pacman.conf \
 && useradd -m -u 1000 tess
