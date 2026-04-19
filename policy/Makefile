# Wrap the distro's SELinux devel Makefile to produce webcasa.pp.
# Requires the selinux-policy-devel package (ships on EL9/EL10, Fedora).
#
#   make          → webcasa.pp
#   make check    → syntax-only
#   make clean    → remove build artifacts

MODULE := webcasa
DEVEL_MK := /usr/share/selinux/devel/Makefile

.PHONY: all check clean

all: $(MODULE).pp

$(MODULE).pp: $(MODULE).te $(MODULE).fc
	@if [ ! -f $(DEVEL_MK) ]; then \
		echo "error: $(DEVEL_MK) missing — install selinux-policy-devel" >&2; \
		exit 1; \
	fi
	$(MAKE) -f $(DEVEL_MK) $@

check:
	@if command -v checkmodule >/dev/null 2>&1; then \
		checkmodule -M -m -o /dev/null $(MODULE).te && echo "$(MODULE).te: syntax OK"; \
	else \
		echo "warning: checkmodule missing — skipping syntax check" >&2; \
	fi

clean:
	@[ -f $(DEVEL_MK) ] && $(MAKE) -f $(DEVEL_MK) clean || rm -f *.mod *.mod.fc tmp/ -rf
	rm -f $(MODULE).pp
