package Bashbrew::RemoteImageRef;
use Mojo::Base -base, -signatures;

# this is modeled after Mojo::URL, but specifically for Docker image references ("perl:5.28", "quay.io/coreos/clair", "bash@sha256:xxx", etc)

use overload '""' => sub { shift->to_string }, fallback => 1;
has [qw( host repo tag digest )];

# https://github.com/docker/distribution/blob/411d6bcfd2580d7ebe6e346359fa16aceec109d5/reference/regexp.go
my $alphaNumericRegexp = qr{ [a-z0-9]+ }x;
my $separatorRegexp = qr{ [._] | __ | [-]* }x;
my $nameComponentRegexp = qr{ $alphaNumericRegexp (?: $separatorRegexp $alphaNumericRegexp )* }x;
my $domainComponentRegexp = qr{ [a-zA-Z0-9] | [a-zA-Z0-9] [a-zA-Z0-9-]* [a-zA-Z0-9] }x;
my $domainRegexp = qr{ $domainComponentRegexp (?: [.] $domainComponentRegexp )* (?: [:] [0-9]+ )? }x;
my $tagRegexp = qr{ [\w] [\w.-]{0,127} }x;
my $digestRegexp = qr{ [A-Za-z] [A-Za-z0-9]* (?: [-_+.] [A-Za-z] [A-Za-z0-9]* )* [:] [[:xdigit:]]{32,} }x;
my $referenceRegexp = qr{
	^
	(
		(?: ($domainRegexp) [/] )?
		($nameComponentRegexp (?: [/] $nameComponentRegexp )*)
	)
	(?: [:] ($tagRegexp) )?
	(?: [@] ($digestRegexp) )?
	$
}x;

# https://github.com/docker/distribution/blob/411d6bcfd2580d7ebe6e346359fa16aceec109d5/reference/reference.go#L37
my $nameTotalLengthMax = 255;

# https://github.com/opencontainers/go-digest/blob/ac19fd6e7483ff933754af248d80be865e543d22/algorithm.go#L55-L61
my $allowedDigestsRegexp = qr{
	^(?:
		sha256:[a-f0-9]{64}
		|
		sha384:[a-f0-9]{96}
		|
		sha512:[a-f0-9]{128}
	)$
}x;

our $DOCKER_HOST = 'docker.io';
our $DOCKER_ORG = 'library';

sub clone {
	my $self  = shift;
	my $clone = $self->new;
	@$clone{keys %$self} = values %$self;
	return $clone;
}

sub new {
	my $self = shift->SUPER::new;
	return $self->parse(@_) if @_;
	return $self;
}

sub parse ($self, $ref) {
	die "'$ref' is not a valid Docker image reference (does not match '$referenceRegexp')" unless $ref =~ $referenceRegexp;
	my ($name, $host, $repo, $tag, $digest) = ($1, $2, $3, $4, $5);
	die "'$name' is too long" if length($name) > $nameTotalLengthMax;
	die "'$digest' is not a valid digest (does not match '$allowedDigestsRegexp')" if $digest and $digest !~ $allowedDigestsRegexp;

	# https://github.com/docker/distribution/blob/411d6bcfd2580d7ebe6e346359fa16aceec109d5/reference/normalize.go#L92-L93
	if ($host && (index($name, '/') == -1 || (index($host, '.') == -1 && index($host, ':') == -1 && $host ne 'localhost'))) {
		$repo = join '/', $host, ($repo // ());
		$host = undef;
	}
	# https://github.com/docker/distribution/blob/411d6bcfd2580d7ebe6e346359fa16aceec109d5/reference/normalize.go#L98-L100
	if (($host // '') eq 'index.docker.io') {
		$host = undef;
	}
	$self->host($host);
	# https://github.com/docker/distribution/blob/411d6bcfd2580d7ebe6e346359fa16aceec109d5/reference/normalize.go#L101-L103
	if ($host && !$self->docker_host && index($repo, '/') == -1) {
		$repo = $DOCKER_ORG . '/' . $repo;
	}

	return $self->repo($repo)->tag($tag)->digest($digest);
}

sub canonical_host ($self) {
	return lc($self->host // '') || $DOCKER_HOST;
}
sub docker_host ($self) {
	return if $self->canonical_host eq $DOCKER_HOST;
	return $self->host;
}
sub canonical_repo ($self) {
	if (!$self->docker_host && index($self->repo, '/') == -1) {
		return $DOCKER_ORG . '/' . $self->repo;
	}
	return $self->repo;
}
sub docker_repo ($self) {
	my $dockerPrefix = $DOCKER_ORG . '/';
	if (!$self->docker_host && index($self->repo, $dockerPrefix) == 0 && index($self->repo, '/', length($dockerPrefix)) == -1) {
		return substr($self->repo, length($dockerPrefix));
	}
	return $self->repo;
}
sub canonical_tag ($self) {
	my $tag = $self->tag;
	$tag = 'latest' unless $tag or $self->digest;
	$tag = undef if ($tag // '') eq 'latest' and $self->digest;
	return $tag;
}
sub _tag ($self) { $self->tag ? ':' . $self->tag : '' }
sub _digest ($self) { $self->digest ? '@' . $self->digest : '' }
sub _tag_canonical ($self) {
	my $tag = $self->canonical_tag;
	return ($tag ? ':' . $tag : '') . $self->_digest;
}

# digest || tag || 'latest' (ie, which ref to fetch from a remote registry)
sub obj ($self) {
	return $self->digest || $self->tag || 'latest';
}
# the last portion of the repo ("foo/bar/baz" => "baz")
sub repo_name ($self) {
	my $lastSlash = rindex($self->repo, '/');
	return $self->repo if $lastSlash == -1;
	return substr($self->repo, $lastSlash + 1);
}
# the rest of the repo name ("foo/bar/baz" => "foo/bar")
sub repo_org ($self) {
	my $lastSlash = rindex($self->repo, '/');
	return if $lastSlash == -1;
	return substr($self->repo, 0, $lastSlash);
}

# host + repo
sub docker_name ($self) {
	return join '/', ($self->docker_host ? $self->docker_host : ()), $self->docker_repo;
}
sub canonical_name ($self) {
	return $self->canonical_host . '/' . $self->canonical_repo;
}

sub to_string ($self) {
	return $self->docker_name . $self->_tag . $self->_digest;
}

# https://github.com/containerd/containerd/blob/7acdb16882080edbe939997e8ed09d7ef3a02cc6/reference/reference.go
# https://github.com/docker/distribution/blob/411d6bcfd2580d7ebe6e346359fa16aceec109d5/reference/normalize.go#L59
sub to_canonical_string ($self) {
	return $self->canonical_name . $self->_tag_canonical;
}

# https://github.com/containerd/containerd/blob/7c1e88399ec0b0b077121d9d5ad97e647b11c870/remotes/docker/resolver.go#L102-L108
sub registry_host ($self) {
	if ($self->canonical_host eq $DOCKER_HOST) {
		return 'registry-1.docker.io';
	}
	return $self->host;
}

1;
