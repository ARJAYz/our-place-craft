from PIL import Image, ImageDraw
src=Image.open('/mnt/user-data/uploads/Our_Place_Craft_Assets/01_backgrounds/hero_world_banner_2x.png').convert('RGBA')
import sys
x,y,s=map(int,sys.argv[1:4])
S=256
art=src.crop((x,y,x+s,y+s)).resize((S-12,S-12),Image.LANCZOS)
bg=Image.new('RGBA',(S,S),(0,0,0,0))
mask=Image.new('L',(S-12,S-12),0); ImageDraw.Draw(mask).rounded_rectangle([0,0,S-13,S-13],radius=44,fill=255)
bg.paste(art,(6,6),mask)
ImageDraw.Draw(bg).rounded_rectangle([6,6,S-7,S-7],radius=46,outline=(63,134,217,255),width=6)
bg.save('icon_preview.png')
bg.save('icon.ico',sizes=[(s,s) for s in [16,20,24,32,40,48,64,128,256]])
