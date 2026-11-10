import random
from PIL import Image, ImageDraw, ImageFilter
random.seed(7)
G=32
spr=Image.new('RGBA',(G,G),(0,0,0,0)); px=spr.load()
grass=[(95,170,60),(108,186,66),(86,156,52),(120,196,74)]
dirt=[(134,96,62),(121,85,54),(148,108,70),(108,76,48)]
def shade(c,k): return tuple(int(v*k) for v in c)+(255,)
for y in range(G):
  for x in range(G):
    cx,cy=x+.5,y+.5
    if abs(cx-16)/12+abs(cy-10)/6<=1:
      px[x,y]=shade(random.choice(grass),1.0); continue
    if 4<=cx<16:
      t=10+(cx-4)/2; k=.72
    elif 16<=cx<28:
      t=16-(cx-16)/2; k=.88
    else: continue
    if t<=cy<=t+13:
      d=cy-t
      g = d<3 or (d<4.5 and random.random()<.5)
      px[x,y]=shade(random.choice(grass if g else dirt),k)
# dark outline
out=Image.new('RGBA',(G,G),(0,0,0,0)); op=out.load()
for y in range(G):
  for x in range(G):
    if px[x,y][3]==0 and any(0<=x+dx<G and 0<=y+dy<G and px[x+dx,y+dy][3] for dx,dy in((1,0),(-1,0),(0,1),(0,-1))):
      op[x,y]=(14,10,24,255)
out.alpha_composite(spr)
S=256
bg=Image.new('RGBA',(S,S),(0,0,0,0))
grad=Image.new('RGBA',(S,S))
gd=ImageDraw.Draw(grad)
for y in range(S):
  t=y/S; c=tuple(int(a+(b-a)*t) for a,b in zip((24,52,92),(9,18,34)))
  gd.line([(0,y),(S,y)],fill=c+(255,))
mask=Image.new('L',(S,S),0); ImageDraw.Draw(mask).rounded_rectangle([6,6,S-7,S-7],radius=48,fill=255)
bg.paste(grad,(0,0),mask)
glow=Image.new('RGBA',(S,S),(0,0,0,0)); ImageDraw.Draw(glow).ellipse([40,40,S-40,S-40],fill=(47,140,255,110))
glow=glow.filter(ImageFilter.GaussianBlur(30))
bg.alpha_composite(Image.composite(glow,Image.new('RGBA',(S,S),(0,0,0,0)),mask))
ImageDraw.Draw(bg).rounded_rectangle([6,6,S-7,S-7],radius=48,outline=(63,134,217,255),width=6)
cube=out.resize((G*6,G*6),Image.NEAREST)
bg.alpha_composite(cube,((S-G*6)//2,(S-G*6)//2+4))
bg.save('icon_preview.png')
sizes=[16,20,24,32,40,48,64,128,256]
bg.save('icon.ico',sizes=[(s,s) for s in sizes])
