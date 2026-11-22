#!/bin/sh
# Gives this checkout's builds a code signing identity that stays the same
# from one build to the next, so that macOS keeps what you allow them: the
# privacy access you grant the app (Full Disk Access, Automation, folders),
# and the firewall's Allow for the command. A build signed ad hoc is a new
# program to macOS each time, so it asks again after every install
# (DESIGN §4.11, Raphael's look at macOS's alerts).
#
#   tools/sign-identity.sh            make "<Name> local code signing" on
#                                     this Mac, if it has none, and sign
#                                     every build with it from now on
#   tools/sign-identity.sh NAME       sign with NAME, an identity you have,
#                                     such as "Apple Development: …" or
#                                     "Developer ID Application: …"
#   tools/sign-identity.sh --keychain PATH
#                                     make it in that keychain, not the
#                                     login keychain (for a test)
#
# The identity it makes is self-signed: a certificate for code signing
# alone, with its private key only in your keychain, which only codesign may
# use without asking. macOS doesn't trust it, which codesign doesn't mind:
# it is to keep the signature the same, not to say who made the build.
# tools/live.sh and mac/build.sh sign with what git config irie.sign names,
# here in the checkout you install from. After the next install, allow
# each access once more, and macOS keeps it.
set -eu
cd "$(git rev-parse --show-toplevel)"
eval "$(tools/brand.sh)"

keychain=
name=
while [ $# -gt 0 ]; do
	case $1 in
	--keychain)
		keychain=${2:?--keychain needs a path}
		shift 2
		;;
	-h | --help)
		sed -n '2,24p' "$0" | sed 's/^# \{0,1\}//'
		exit 0
		;;
	*)
		name=$1
		shift
		;;
	esac
done

# has says whether the keychain holds the identity $name. Without -v, as
# -v leaves out an identity macOS doesn't trust, as a self-signed one.
has() {
	if [ -n "$keychain" ]; then
		security find-identity -p codesigning "$keychain"
	else
		security find-identity -p codesigning
	fi | grep -qF "\"$name\""
}

made=
if [ -z "$name" ]; then
	name="$BRAND_NAME local code signing"
	if ! has; then
		work=$(mktemp -d)
		trap 'rm -rf "$work"' EXIT
		cat >"$work/req.cnf" <<-EOF
			[req]
			distinguished_name = dn
			prompt = no
			x509_extensions = ext
			[dn]
			CN = $name
			[ext]
			basicConstraints = critical, CA:false
			keyUsage = critical, digitalSignature
			extendedKeyUsage = critical, codeSigning
			subjectKeyIdentifier = hash
		EOF
		# macOS's own openssl (LibreSSL) writes a PKCS #12 file that
		# security(1) reads; OpenSSL 3's defaults it can't.
		/usr/bin/openssl req -x509 -newkey rsa:2048 -nodes -days 3650 -sha256 \
			-config "$work/req.cnf" -keyout "$work/key.pem" -out "$work/cert.pem" 2>"$work/openssl.log" ||
			{ cat "$work/openssl.log" >&2; exit 1; }
		pass=$(/usr/bin/openssl rand -hex 16)
		/usr/bin/openssl pkcs12 -export -inkey "$work/key.pem" -in "$work/cert.pem" -name "$name" \
			-out "$work/id.p12" -passout "pass:$pass"
		# -T lets codesign use the key without asking each time.
		security import "$work/id.p12" -k "${keychain:-$HOME/Library/Keychains/login.keychain-db}" \
			-P "$pass" -T /usr/bin/codesign >/dev/null
		made=1
	fi
fi
if ! has; then
	echo "sign-identity: no code signing identity \"$name\" in the keychain" >&2
	exit 1
fi
if [ -n "$keychain" ]; then
	what=Found
	[ -z "$made" ] || what=Made
	echo "$what \"$name\" in $keychain; git config is left as it was."
	exit 0
fi
git config irie.sign "$name"
if [ -n "$made" ]; then
	echo "Made \"$name\" in your login keychain."
fi
echo "Every build tools/live.sh installs from here is signed with \"$name\" now (git config irie.sign)."
echo "After the next install, allow each access $BRAND_NAME asks for once more, and macOS keeps it."
