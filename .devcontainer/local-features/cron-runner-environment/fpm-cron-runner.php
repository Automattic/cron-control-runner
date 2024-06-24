<?php

namespace WPCLI\FPM;
error_reporting( 0 );

if ( isset( $_POST['payload'] ) ) {
	$payload_str = $_POST['payload'];
	unset( $_POST['payload'] );
} elseif ( isset ( $_GET['payload'] ) ) {
	$payload_str = $_GET['payload'];
	unset( $_GET['payload'] );
} else {
	header( 'Status: 400 Bad Request' );
	header( 'Content-Type: text/plain' );
	echo 'no payload given' . "\n";
	exit( 1 );
}

try {
	$payload = json_decode( $payload_str, null, 512, JSON_THROW_ON_ERROR | JSON_OBJECT_AS_ARRAY );
	if ( ! is_array( $payload ) ) {
		throw new \Exception( "not a json array" );
	}
} catch ( \Exception $e ) {
	header( 'Status: 400 Bad Request' );
	header( 'Content-Type: text/plain' );
	echo 'payload cannot be decoded as json: ' . $e->getMessage() . "\n";
	exit( 1 );
}

array_unshift( $payload, '/usr/local/bin/wp' );

global $_SERVER;
$_SERVER['argv'] = $payload;
$_SERVER['SCRIPT_NAME'] = '/usr/local/bin/wp';
$_SERVER['SCRIPT_FILENAME'] = '/usr/local/bin/wp';
unset( $_SERVER['FCGI_ROLE'] );
unset( $_SERVER['GATEWAY_INTERFACE'] );
unset( $_SERVER['QUERY_STRING'] );
unset( $_SERVER['REQUEST_METHOD'] );

global $_ENV;
$_ENV['SCRIPT_NAME'] = '/usr/local/bin/wp';
$_ENV['SCRIPT_FILENAME'] = '/usr/local/bin/wp';
unset( $_ENV['FCGI_ROLE'] );
unset( $_ENV['GATEWAY_INTERFACE'] );
unset( $_ENV['QUERY_STRING'] );
unset( $_ENV['REQUEST_METHOD'] );

global $argv;
$argv            = $payload;

$outfh           = tmpfile();
$errfh           = tmpfile();

register_shutdown_function( function () use ( $outfh, $errfh ) {
	$result = [
		'buf' => ob_get_contents(),
	];
	ob_end_clean();
	fseek( $outfh, 0 );
	$result['stdout'] = stream_get_contents( $outfh );
	fclose( $outfh );
	fseek( $errfh, 0 );
	$result['stderr'] = stream_get_contents( $errfh );
	fclose( $errfh );
	header( 'Status: 200 OK' );
	header( 'Content-Type: application/json' );
	echo json_encode( $result );
} );

define( 'WP_CLI_ROOT', 'phar:///usr/local/bin/wp.phar/vendor/wp-cli/wp-cli' );
define( 'STDIN', fopen( '/dev/null', 'r' ) );
define( 'STDOUT', $outfh );
define( 'STDERR', $errfh );

ob_start();

require_once WP_CLI_ROOT . '/php/wp-cli.php';

exit( 0 );
