# Exportar

## Formatos

- **PNG**: imagen fija del frame actual. Es el único formato que respeta el fondo
  transparente de forma fiel.
- **GIF**: animación en bucle con paleta limitada a 256 colores. Es el formato más
  compatible para compartir rápido, pero los degradados y la acuarela muestran bandas.
- **WebM**: video con compresión moderna, mejor calidad y menor peso que GIF para
  animaciones largas o con muchos colores.

## Escala

La escala de exportación se elige entre **1x** y **2x**, y multiplica el tamaño del
canvas. El resultado tiene un tope de **1920 × 1080 píxeles**: si el canvas es más
grande, la escala se reduce automáticamente para no superar el tope y la app avisa
"la exportación se ajustó al tamaño máximo".

Ejemplos: un canvas de 960 × 540 exportado a 2x da 1920 × 1080, que es exactamente el
tope. Un canvas de 1280 × 720 a 2x daría 2560 × 1440 y se recorta a 1920 × 1080.

## Duración y FPS

Para GIF y WebM se configuran:

- **duración**: de 0.5 a 30 segundos
- **FPS**: de 1 a 60, con 12 como valor por defecto

El GIF no permite menos de 5 FPS de forma estable: por debajo, los navegadores
reproducen más lento de lo configurado. Para animaciones lentas conviene WebM.

## Fondo

- **sólido**: usa el color de fondo del canvas
- **transparente**: disponible en PNG y WebM; en GIF la transparencia es de un solo
  color y no admite semitransparencia, así que los bordes suaves se ven con dientes de
  sierra

## Qué se exporta

Solo las capas visibles. Las capas ocultas y el zoom del viewport no afectan el
resultado. El hervido se exporta tal como está configurado; si está apagado con la
tecla A, todos los frames salen idénticos y la animación queda estática.
