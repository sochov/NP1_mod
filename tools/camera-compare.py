"""Decode raw export variants for engineering color/format QA; private outputs stay in tmp."""
import pathlib
import subprocess
root=pathlib.Path(__file__).resolve().parents[1]
for source in ('in','front'):
    for fmt in ('nv12','nv21','yuv420p','yvu420p'):
        output=root/'tmp'/('camera-'+source+'-'+fmt+'.png')
        result=subprocess.run(['ffmpeg','-hide_banner','-loglevel','error','-y','-f','rawvideo','-pixel_format',fmt,'-video_size','1920x1080','-i',str(root/'tmp'/('camera-'+source+'.raw')),'-frames:v','1','-vf','scale=640:360',str(output)],capture_output=True)
        if result.returncode:
            print(fmt,'not supported by this ffmpeg build')
print('Private format comparisons generated in tmp/')
