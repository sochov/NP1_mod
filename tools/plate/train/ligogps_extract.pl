#!/usr/bin/perl
# Extract LIGOGPSINFO GPS samples from dash-camera MP4 files using ExifTool's
# LigoGPS decoder (ExifTool >= 13 is required; pass its lib directory).
#
#   perl tools/plate/train/ligogps_extract.pl EXIFTOOL_LIB clip.MP4 [...]
#
# Prints CSV: file,sample,gps_datetime,lat,lon,speed_kmh
use strict;
use warnings;

my $lib = shift @ARGV or die "usage: $0 EXIFTOOL_LIB clip.MP4 ...\n";
unshift @INC, $lib;
require Image::ExifTool;
require Image::ExifTool::LigoGPS;
require Image::ExifTool::QuickTime;

package Collector;
our @ISA = ('Image::ExifTool');
our ($file, $n, %row);
sub HandleTag {
    my ($self, $tbl, $tag, $val) = @_;
    if ($tag eq 'GPSDateTime') { %row = (time => $val); }
    elsif ($tag eq 'GPSLatitude') { $row{lat} = $val; }
    elsif ($tag eq 'GPSLongitude') { $row{lon} = $val; }
    elsif ($tag eq 'GPSSpeed') {
        $row{spd} = $val;
        printf "%s,%d,%s,%.7f,%.7f,%.2f\n", $file, $n, $row{time}, $row{lat}, $row{lon}, $row{spd} if defined $row{lat};
    }
    return undef;
}
sub Warn { return 1; }

package main;
print "file,sample,gps_datetime,lat,lon,speed_kmh\n";
foreach my $clip (@ARGV) {
    # The GPS samples live in a timed data track; ffmpeg copies every sample.
    my @streams = `ffprobe -v error -select_streams d -show_entries stream=index -of csv=p=0 "$clip"`;
    my $data = '';
    foreach my $index (map { s/\s+//gr } @streams) {
        my $raw = `ffmpeg -v error -i "$clip" -map 0:$index -c copy -f data - 2>/dev/null`;
        if (index($raw, 'LIGOGPSINFO') >= 0) { $data = $raw; last; }
    }
    next unless length $data;
    my $et = Collector->new;
    $$et{OPTIONS}{Verbose} = 0;
    my $tbl = Image::ExifTool::GetTagTable('Image::ExifTool::QuickTime::Stream');
    $Collector::file = $clip;
    $Collector::n = 0;
    foreach my $chunk (split /(?=LIGOGPSINFO)/, $data) {
        next unless $chunk =~ /^LIGOGPSINFO/;
        $Collector::n++;
        Image::ExifTool::LigoGPS::ProcessLigoGPS($et, { DataPt => \$chunk, DirName => 'LigoGPS' }, $tbl);
    }
}
