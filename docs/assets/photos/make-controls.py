#!/usr/bin/env python3
"""Draws nanokontrol2-controls.png: the controller photo with the controls marked
by what they do in the current version, and a legend.

Update the legend texts and colours when a phase is released, then run it from
the repository root:

    python3 docs/assets/photos/make-controls.py

Needs Pillow (pip install pillow) and the DejaVu fonts (fonts-dejavu-core on
Debian/Ubuntu). The result keeps the photo's license, CC BY-NC-SA 3.0 (see
README.md in this folder).
"""
import os
from PIL import Image, ImageDraw, ImageFont

HERE=os.path.dirname(os.path.abspath(__file__))
SRC=os.path.join(HERE,'nanokontrol2.png')
OUT=os.path.join(HERE,'nanokontrol2-controls.png')
photo=Image.open(SRC).convert('RGBA')
S=2  # supersample
W,PH=1600,389
PAD_T, PAD_B, LEG = 30, 30, 250
H=PAD_T+PH+PAD_B+LEG
INDIGO=(35,58,94); CREAM=(239,228,206); INK=(42,42,46)
GREEN=(76,195,138); AMBER=(242,184,75); VERM=(232,120,92); GREY=(160,165,170)
F='/usr/share/fonts/truetype/dejavu/'  # fonts-dejavu-core
def font(sz,b=False): return ImageFont.truetype(F+('DejaVuSans-Bold.ttf' if b else 'DejaVuSans.ttf'), sz*S)
img=Image.new('RGBA',(W*S,H*S),INDIGO+(255,))
img.alpha_composite(photo.resize((W*S,PH*S),Image.LANCZOS),(0,PAD_T*S))
d=ImageDraw.Draw(img)
def box(x0,y0,x1,y1,col,r=14):
    y0+=PAD_T; y1+=PAD_T
    d.rounded_rectangle([x0*S,y0*S,x1*S,y1*S],r*S,outline=INDIGO,width=8*S)
    d.rounded_rectangle([x0*S,y0*S,x1*S,y1*S],r*S,outline=col,width=4*S)
def dim(x0,y0,x1,y1,r=10):
    y0+=PAD_T; y1+=PAD_T
    ov=Image.new('RGBA',img.size,(0,0,0,0))
    ImageDraw.Draw(ov).rounded_rectangle([x0*S,y0*S,x1*S,y1*S],r*S,fill=INDIGO+(120,))
    img.alpha_composite(ov)
def badge(n,x,y,col):
    y+=PAD_T; R=17
    d.ellipse([(x-R)*S,(y-R)*S,(x+R)*S,(y+R)*S],fill=col,outline=INDIGO,width=3*S)
    d.text((x*S,y*S),str(n),font=font(20,True),fill=INDIGO,anchor='mm')
# Frames on the photo, in photo pixels (1600 x 389). Colours match the legend.
# 1 working: knobs, sliders, S, M
box(603,40,1592,352,GREEN,18); badge(1,603,40,GREEN)
# 2 R buttons
for i in range(8):
    x=613+i*118
    dim(x-4,276,x+44,327); box(x-5,275,x+45,328,GREY,10)
badge(2,597,301,GREY)
# 3 media keys
box(247,267,509,333,AMBER); badge(3,247,333,AMBER)
# 4 layouts: track + cycle
box(247,160,377,256,VERM); badge(4,247,160,VERM)
# 5 marker + record
dim(381,211,577,257,12); dim(514,268,575,332); box(380,210,578,258,GREY,12); box(513,267,576,333,GREY,10); badge(5,578,210,GREY)
# Legend: edit the colours and texts here when a phase is released.
items=[(1,GREEN,'Works now','Sliders and knobs set each app’s volume; S solos, M mutes; the LEDs show it.'),
       (2,GREY,'No function yet','R buttons. Lit on an input column (e.g. your microphone).'),
       (3,AMBER,'Planned for 0.2.0','◀◀ ▶▶ ■ ▶ control the music player that is playing.'),
       (4,VERM,'Planned for 0.4.0','Track ◀ ▶ and Cycle switch between layouts.'),
       (5,GREY,'Not assigned yet','Marker buttons and Record: ideas in the roadmap’s backlog.')]
y0=PAD_T+PH+PAD_B
colx=[60,840]
for k,(n,col,title,text) in enumerate(items):
    cx=colx[k%2] if k<4 else colx[0]; cy=y0+(k//2)*75+10
    R=17
    d.ellipse([(cx-R)*S,(cy-R+12)*S,(cx+R)*S,(cy+R+12)*S],fill=col)
    d.text((cx*S,(cy+12)*S),str(n),font=font(20,True),fill=INDIGO,anchor='mm')
    d.text(((cx+32)*S,cy*S),title,font=font(22,True),fill=CREAM)
    d.text(((cx+32)*S,(cy+32)*S),text,font=font(18),fill=CREAM+(255,))
d.text((W*S-20*S,(H-14)*S),'Photo: jzohsuh / iFixit, CC BY-NC-SA 3.0, annotated',font=font(14),fill=(147,152,156),anchor='rs')
out=img.resize((W,H),Image.LANCZOS).convert('RGB')
out.save(OUT,optimize=True)
print('wrote',OUT,out.size)
