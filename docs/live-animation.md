# Animación viva

## Qué es el hervido

El hervido (en inglés *boil* o *wiggle*) es el efecto que hace que un dibujo parezca
redibujado a mano cuadro por cuadro, como en la animación tradicional. En lugar de
tener frames distintos, el motor toma los trazos y les aplica pequeñas variaciones
aleatorias que cambian con el tiempo.

El resultado es esa vibración característica del dibujo animado: las líneas nunca
quedan quietas del todo.

## Parámetros del hervido

El hervido se configura por trazo y tiene tres parámetros:

- **jiggle**: amplitud de la deformación, en píxeles. Valores bajos (1 a 3) dan un
  temblor sutil; valores altos (8 o más) dan un aspecto tembloroso y nervioso.
- **velocidad**: cada cuánto cambia la variación. Se expresa en variaciones por
  segundo; el rango útil va de 2 a 24. A 12 se siente como animación a 12 fps.
- **desync**: desfase entre trazos. Con desync en 0 todos los trazos vibran al mismo
  tiempo; con desync alto cada trazo cambia en un momento distinto, lo que se ve
  mucho más orgánico.

## Motores de redibujado y cinética

El motor de redibujado decide **cuándo** se recalcula la variación del hervido:

- **motor de tiempo fijo**: recalcula a una frecuencia constante, independiente de la
  velocidad del trazo. Es el modo estable y predecible.
- **motor cinético**: recalcula en función del movimiento. Si el trazo está quieto, la
  variación se congela; si te movés rápido, la variación se acelera.

La cinética da una sensación más viva pero puede generar cambios bruscos cuando el
trazo se detiene de golpe. Para exportar, el modo de tiempo fijo da resultados más
consistentes.

## Atajo y alcance

La tecla **A** prende y apaga el hervido de toda la escena. Apagarlo no borra la
configuración de cada trazo: al volver a prenderlo, cada trazo recupera su jiggle,
velocidad y desync.

El hervido es un efecto de visualización: no modifica los puntos originales del
trazo, así que deshacer no lo revierte. Si exportás con el hervido apagado, el
archivo sale sin variación entre frames.
