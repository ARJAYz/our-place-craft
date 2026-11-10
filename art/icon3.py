from PIL import Image, ImageDraw, ImageChops, ImageFilter
im=Image.open('/mnt/user-data/uploads/Desktop/Gemini_Generated_Image_hra1tyhra1tyhra1.jpg').convert('RGB')
bgc=im.getpixel((5,5))
diff=ImageChops.difference(im,Image.new('RGB',im.size,bgc)).convert('L').point(lambda v:255 if v>18 else 0)
box=diff.getbbox(); print(box, bgc)
c=im.crop(box); w,h=c.size; s=max(w,h)
sq=Image.new('RGB',(s,s),bgc); sq.paste(c,((s-w)//2,(s-h)//2))
# alpha: flood-fill the background from the corners
m=Image.new('L',(s,s),255); d=diff.crop(box); dm=Image.new('L',(s,s),0); dm.paste(d,((s-w)//2,(s-h)//2))
ff=dm.copy()
for p in [(0,0),(s-1,0),(0,s-1),(s-1,s-1)]:
    ImageDraw.floodfill(ff,p,128)
alpha=ff.point(lambda v:0 if v==128 else 255).filter(ImageFilter.GaussianBlur(1.2))
out=sq.convert('RGBA'); out.putalpha(alpha)
out=out.resize((256,256),Image.LANCZOS)
out.save('icon_preview.png')
out.save('icon.ico',sizes=[(x,x) for x in [16,20,24,32,40,48,64,128,256]])
