FROM alpine:3.24.4
RUN apk add --no-cache \
      dbus gnome-keyring keyutils xvfb xclip xsel wl-clipboard sway swaybg grim mesa-dri-gallium \
 && adduser -D -u 1000 tess
