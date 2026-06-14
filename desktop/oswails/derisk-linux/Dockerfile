FROM ubuntu:24.04
ENV DEBIAN_FRONTEND=noninteractive
# Wails Linux build + runtime deps: GTK3 + WebKitGTK 4.1 (24.04 has 4.1, not 4.0),
# plus a headless X stack (xvfb), dbus (WebKitGTK needs a session bus), and
# imagemagick for `import` screenshots of the virtual root window.
RUN apt-get update && apt-get install -y --no-install-recommends \
      gcc g++ pkg-config ca-certificates curl git \
      libgtk-3-dev libwebkit2gtk-4.1-dev \
      xvfb x11-utils x11-apps imagemagick \
      dbus dbus-x11 iproute2 procps \
      fonts-dejavu-core fonts-liberation \
 && rm -rf /var/lib/apt/lists/*
# Go 1.26.2 (Ubuntu's packaged Go is too old for this module's go 1.25.10 directive).
RUN curl -fsSL https://go.dev/dl/go1.26.2.linux-arm64.tar.gz | tar -C /usr/local -xz
ENV PATH=/usr/local/go/bin:/root/go/bin:$PATH
ENV GOTOOLCHAIN=local
WORKDIR /src
