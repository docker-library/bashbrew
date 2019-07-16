package Bashbrew;
use Mojo::Base -base, -signatures;

use version; our $VERSION = qv(0.0.1); # TODO useful version number?

use Exporter 'import';
our @EXPORT_OK = qw(
	arch_to_platform
	bashbrew
);

# TODO create dedicated Bashbrew::Arch package?
sub arch_to_platform ($arch) {
	if ($arch =~ m{
		^
		(?: ([^-]+) - )? # optional "os" prefix ("windows-", etc)
		([^-]+?) # "architecture" bit ("arm64", "s390x", etc)
		(v[0-9]+)? # optional "variant" suffix ("v7", "v6", etc)
		$
	}x) {
		return (
			os => $1 // 'linux',
			architecture => (
				$2 eq 'i386'
				? '386'
				: (
					$2 eq 'arm32'
					? 'arm'
					: $2
				)
			),
			($3 ? (variant => $3) : ()),
		);
	}
	die "unrecognized architecture format in: $arch";
}

# TODO make this promise-based and non-blocking? (and/or make a dedicated Package for it?)
# https://github.com/jberger/Mojolicious-Plugin-TailLog/blob/master/lib/Mojolicious/Plugin/TailLog.pm#L16-L22
# https://metacpan.org/pod/Capture::Tiny
# https://metacpan.org/pod/Mojo::IOLoop#subprocess
# https://metacpan.org/pod/IO::Async::Process
# (likely not worth it, given how quickly it typically completes)
sub bashbrew (@) {
	open my $fh, '-|', 'bashbrew', @_ or die "failed to run 'bashbrew': $!";
	local $/;
	my $output = <$fh>;
	close $fh or die "failed to close 'bashbrew'";
	chomp $output;
	return $output;
}

1;
