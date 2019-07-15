use 5.010001;

use strict;
use warnings;

use ExtUtils::MakeMaker;

# https://perldoc.perl.org/ExtUtils/MakeMaker.html#Using-Attributes-and-Parameters
WriteMakefile(
	NAME => 'Bashbrew',
	VERSION_FROM => 'lib/Bashbrew.pm',
	TEST_REQUIRES => {
		'Test::More' => 0,
	},
	PREREQ_PM => {
		'EV' => 0,
		'IO::Socket::IP' => 0,
		'IO::Socket::SSL' => 0,
		'IO::Socket::Socks' => 0,
		'Net::DNS::Native' => 0,
# https://metacpan.org/release/Mojolicious
		'Mojolicious' => '== 8.20',
	},
	# TODO EXE_FILES ?
);
