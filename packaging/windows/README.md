# Windows packaging status

Windows service and Wintun integration are **Planned**. The current cross-build
contains an explicit `not implemented` TUN factory. A future WiX-based installer
will install under `Program Files\TyxNet`, register an Automatic Windows Service,
configure fixed firewall rules, and install Wintun only under its redistribution
terms. No Wintun binary is bundled today.
